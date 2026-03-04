import datetime
import importlib
import time


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
