import datetime
import concurrent.futures
import importlib
import time

import pytest


def _reload_connect_module():
    connect_module = importlib.import_module("gauntlet.connect")
    return importlib.reload(connect_module)


def test_connect_noop_when_disabled(monkeypatch):
    monkeypatch.delenv("GAUNTLET_ENABLED", raising=False)

    connect_module = _reload_connect_module()
    original_datetime = datetime.datetime
    original_time = time.time

    connect_module.connect()

    assert connect_module._connected is False
    assert datetime.datetime is original_datetime
    assert time.time is original_time


def test_connect_patches_time_and_disconnect_restores(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    monkeypatch.setenv("GAUNTLET_FREEZE_TIME", "2025-01-15T10:00:00Z")

    connect_module = _reload_connect_module()
    original_datetime = datetime.datetime
    original_time = time.time
    original_localtime = time.localtime

    connect_module.connect()

    assert connect_module._connected is True
    assert len(connect_module._patchers) >= 3
    assert datetime.datetime is not original_datetime
    assert time.time is not original_time
    assert time.localtime is not original_localtime

    now = datetime.datetime.now()
    assert now.year == 2025
    assert now.month == 1
    assert now.day == 15

    connect_module.disconnect()

    assert connect_module._connected is False
    assert connect_module._patchers == []
    assert datetime.datetime is original_datetime
    assert time.time is original_time
    assert time.localtime is original_localtime


def test_connect_is_idempotent(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    monkeypatch.setenv("GAUNTLET_FREEZE_TIME", "2025-01-15T10:00:00Z")

    connect_module = _reload_connect_module()
    connect_module.connect()
    patch_count = len(connect_module._patchers)

    connect_module.connect()

    assert len(connect_module._patchers) == patch_count
    connect_module.disconnect()


def test_connect_thread_safe_single_connected_state(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    monkeypatch.setenv("GAUNTLET_FREEZE_TIME", "2025-01-15T10:00:00Z")

    connect_module = _reload_connect_module()

    def _call_connect(_):
        connect_module.connect()
        return len(connect_module._patchers)

    with concurrent.futures.ThreadPoolExecutor(max_workers=20) as executor:
        patch_counts = list(executor.map(_call_connect, range(20)))

    assert connect_module._connected is True
    assert len(connect_module._patchers) >= 4
    assert all(count == len(connect_module._patchers) for count in patch_counts)
    connect_module.disconnect()


def test_connect_keeps_monotonic_advancing(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    monkeypatch.setenv("GAUNTLET_FREEZE_TIME", "2025-01-15T10:00:00Z")

    connect_module = _reload_connect_module()
    connect_module.connect()

    first = time.monotonic()
    time.sleep(0.01)
    second = time.monotonic()
    assert second > first

    connect_module.disconnect()


def test_connect_forces_proxy_env_for_loopback(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7431")
    monkeypatch.setenv("NO_PROXY", "localhost,127.0.0.1")
    monkeypatch.setenv("no_proxy", "localhost,127.0.0.1")

    connect_module = _reload_connect_module()
    connect_module.connect()

    assert connect_module._connected is True
    assert connect_module.os.environ.get("ALL_PROXY") == "http://127.0.0.1:7431"
    assert connect_module.os.environ.get("all_proxy") == "http://127.0.0.1:7431"
    assert connect_module.os.environ.get("NO_PROXY") == ""
    assert connect_module.os.environ.get("no_proxy") == ""

    connect_module.disconnect()


def test_connect_patches_httpx_loopback_proxy_if_available(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7431")

    connect_module = _reload_connect_module()
    connect_module.connect()

    try:
        import httpx
    except ImportError:
        connect_module.disconnect()
        pytest.skip("httpx not installed")

    client = httpx.Client()
    proxies = getattr(client, "proxies", None)
    client.close()
    connect_module.disconnect()

    assert proxies is not None
    if isinstance(proxies, dict):
        assert proxies.get("all://") == "http://127.0.0.1:7431"
    else:
        assert "127.0.0.1:7431" in str(proxies)


def test_connect_emits_sdk_capability_report(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")

    connect_module = _reload_connect_module()
    events_module = importlib.import_module("gauntlet.events")

    captured = []
    monkeypatch.setattr(
        connect_module,
        "_install_adapter_instrumentation",
        lambda: {
            "openai": {"enabled": True, "patched": False, "reason": "openai_not_installed"}
        },
    )
    monkeypatch.setattr(
        events_module,
        "emit_event",
        lambda event_type, **kwargs: captured.append((event_type, kwargs)),
    )

    connect_module.connect()

    assert connect_module._connected is True
    assert captured
    event_type, payload = captured[0]
    assert event_type == "sdk_capabilities"
    report = payload["result"]
    assert report["protocol_version"] == 1
    assert report["sdk"] == "gauntlet-python"
    assert report["runtime"].startswith("python")
    assert report["adapters"]["openai"]["reason"] == "openai_not_installed"

    connect_module.disconnect()


def test_connect_emits_determinism_env_report(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    monkeypatch.setenv("GAUNTLET_FREEZE_TIME", "2025-01-15T10:00:00Z")
    monkeypatch.setenv("GAUNTLET_LOCALE", "C")
    monkeypatch.setenv("GAUNTLET_TIMEZONE", "UTC")

    connect_module = _reload_connect_module()
    events_module = importlib.import_module("gauntlet.events")

    captured = []
    monkeypatch.setattr(
        connect_module,
        "_install_adapter_instrumentation",
        lambda: {"openai": {"enabled": True, "patched": True}},
    )
    monkeypatch.setattr(
        events_module,
        "emit_event",
        lambda event_type, **kwargs: captured.append((event_type, kwargs)),
    )

    connect_module.connect()

    determinism_events = [payload for event_type, payload in captured if event_type == "determinism_env"]
    assert determinism_events
    report = determinism_events[-1]["result"]
    assert report["language"] == "python"
    assert report["requested_timezone"] == "UTC"
    assert report["effective_timezone"] == "UTC"
    assert report["timezone_applied"] is True
    assert report["requested_locale"] == "C"
    assert report["locale_applied"] is True
    assert report["time_patched"] is True
    assert report["requested_freeze_time"] == "2025-01-15T10:00:00Z"

    connect_module.disconnect()
