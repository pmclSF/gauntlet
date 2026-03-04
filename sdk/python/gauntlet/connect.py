"""Gauntlet connect — initializes SDK hooks when running under Gauntlet."""

import os
import random
import sys
import time
import datetime
import locale
from unittest.mock import patch

_connected = False
_mode = None
_freeze_time = None
_patchers = []
_adapter_status = {}


def is_enabled() -> bool:
    return os.environ.get("GAUNTLET_ENABLED") == "1"


def get_mode() -> str:
    return os.environ.get("GAUNTLET_MODEL_MODE", "recorded")


def connect():
    """Initialize Gauntlet SDK hooks.

    When GAUNTLET_ENABLED=1, this:
    - Patches time/datetime to return frozen time
    - Seeds RNG for determinism
    - Sets locale and timezone
    - No-ops gracefully when not in Gauntlet mode
    """
    global _connected, _mode, _freeze_time, _adapter_status

    if not is_enabled():
        return

    if _connected:
        return

    _mode = get_mode()
    _force_proxy_for_loopback()
    _adapter_status = _install_adapter_instrumentation()
    _emit_capability_report(_adapter_status)

    # Freeze time
    freeze_time_str = os.environ.get("GAUNTLET_FREEZE_TIME")
    time_patched = False
    if freeze_time_str:
        _freeze_time = datetime.datetime.fromisoformat(
            freeze_time_str.replace("Z", "+00:00")
        )
        _patch_time(_freeze_time)
        time_patched = True

    # Seed RNG
    rng_seed = os.environ.get("GAUNTLET_RNG_SEED")
    if rng_seed:
        seed = int(rng_seed)
        random.seed(seed)
        try:
            import numpy

            numpy.random.seed(seed)
        except ImportError:
            pass

    # Set locale
    gauntlet_locale = os.environ.get("GAUNTLET_LOCALE")
    locale_applied = False
    effective_locale = ""
    if gauntlet_locale:
        try:
            locale.setlocale(locale.LC_ALL, gauntlet_locale)
            locale_applied = True
        except locale.Error:
            pass
    try:
        effective_locale = locale.setlocale(locale.LC_ALL)
    except locale.Error:
        effective_locale = ""

    # Set timezone
    tz = os.environ.get("GAUNTLET_TIMEZONE")
    timezone_applied = False
    effective_timezone = os.environ.get("TZ", "")
    if tz:
        os.environ["TZ"] = tz
        effective_timezone = os.environ.get("TZ", "")
        try:
            time.tzset()
            timezone_applied = True
        except AttributeError:
            timezone_applied = os.environ.get("TZ") == tz
    _emit_determinism_env_report(
        freeze_time_requested=freeze_time_str,
        time_patched=time_patched,
        locale_requested=gauntlet_locale,
        locale_applied=locale_applied,
        effective_locale=effective_locale,
        timezone_requested=tz,
        timezone_applied=timezone_applied,
        effective_timezone=effective_timezone,
    )

    _connected = True


def _force_proxy_for_loopback():
    """Force proxy usage for loopback/local calls in supported HTTP clients."""
    proxy = (
        os.environ.get("HTTPS_PROXY")
        or os.environ.get("https_proxy")
        or os.environ.get("HTTP_PROXY")
        or os.environ.get("http_proxy")
    )
    if proxy:
        os.environ["ALL_PROXY"] = proxy
        os.environ["all_proxy"] = proxy

    # Clearing NO_PROXY/no_proxy prevents bypass for localhost/127.0.0.1 in clients
    # that honor these vars directly.
    os.environ["NO_PROXY"] = ""
    os.environ["no_proxy"] = ""


def _patch_time(frozen: datetime.datetime):
    """Patch datetime and time modules to return frozen values."""
    frozen_utc = frozen
    if frozen_utc.tzinfo is None:
        frozen_utc = frozen_utc.replace(tzinfo=datetime.timezone.utc)
    frozen_epoch = frozen_utc.timestamp()
    real_datetime = datetime.datetime
    real_localtime = time.localtime

    real_date = datetime.date

    class FrozenDatetime(real_datetime):
        @classmethod
        def now(cls, tz=None):
            if tz is None:
                return frozen_utc.replace(tzinfo=None)
            return frozen_utc.astimezone(tz)

        @classmethod
        def utcnow(cls):
            return frozen_utc.astimezone(datetime.timezone.utc).replace(
                tzinfo=None
            )

    class FrozenDate(real_date):
        @classmethod
        def today(cls):
            return frozen_utc.date()

    _start_patch("datetime.datetime", FrozenDatetime)
    _start_patch("datetime.date", FrozenDate)
    _start_patch("time.time", lambda: frozen_epoch)
    _start_patch(
        "time.localtime",
        lambda secs=None: real_localtime(
            frozen_epoch if secs is None else secs
        ),
    )
    # Freeze monotonic clock so duration-based logic is deterministic.
    _monotonic_base = time.monotonic()
    _start_patch("time.monotonic", lambda: _monotonic_base)


def disconnect():
    """Undo active patches and reset connect() state."""
    global _connected, _mode, _freeze_time, _patchers, _adapter_status

    for p in reversed(_patchers):
        try:
            p.stop()
        except RuntimeError:
            pass
    _patchers = []
    _connected = False
    _mode = None
    _freeze_time = None
    _adapter_status = {}


def _start_patch(target: str, value):
    patcher = patch(target, new=value)
    patcher.start()
    _patchers.append(patcher)


def _install_adapter_instrumentation():
    """Best-effort SDK adapter instrumentation setup."""
    try:
        from gauntlet.adapters import (
            install_anthropic_instrumentation,
            install_langchain_instrumentation,
            install_openai_instrumentation,
        )
    except Exception:
        return {}

    status = {
        "openai": install_openai_instrumentation(),
        "anthropic": install_anthropic_instrumentation(),
        "langchain": install_langchain_instrumentation(),
    }
    return status


def _emit_capability_report(adapter_status):
    """Emit SDK capability negotiation metadata for runner-side checks."""
    try:
        from gauntlet.events import emit_event
    except Exception:
        return

    payload = {
        "protocol_version": 1,
        "sdk": "gauntlet-python",
        "runtime": f"python{sys.version_info.major}.{sys.version_info.minor}",
        "adapters": adapter_status or {},
    }
    emit_event("sdk_capabilities", result=payload)


def _emit_determinism_env_report(
    freeze_time_requested=None,
    time_patched=False,
    locale_requested=None,
    locale_applied=False,
    effective_locale="",
    timezone_requested=None,
    timezone_applied=False,
    effective_timezone="",
):
    """Emit runtime verification report for determinism environment controls."""
    try:
        from gauntlet.events import emit_event
    except Exception:
        return

    payload = {
        "language": "python",
        "runtime": f"python{sys.version_info.major}.{sys.version_info.minor}",
        "requested_freeze_time": freeze_time_requested or "",
        "time_patched": bool(time_patched),
        "requested_timezone": timezone_requested or "",
        "effective_timezone": effective_timezone or "",
        "timezone_applied": bool(timezone_applied),
        "requested_locale": locale_requested or "",
        "effective_locale": effective_locale or "",
        "locale_applied": bool(locale_applied),
    }
    emit_event("determinism_env", result=payload)
