"""Gauntlet connect — initializes SDK hooks when running under Gauntlet."""

import os
import random
import time
import datetime
import locale

_connected = False
_mode = None
_freeze_time = None


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
    global _connected, _mode, _freeze_time

    if not is_enabled():
        return

    if _connected:
        return

    _mode = get_mode()

    # Freeze time
    freeze_time_str = os.environ.get("GAUNTLET_FREEZE_TIME")
    if freeze_time_str:
        _freeze_time = datetime.datetime.fromisoformat(
            freeze_time_str.replace("Z", "+00:00")
        )
        _patch_time(_freeze_time)

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
    if gauntlet_locale:
        try:
            locale.setlocale(locale.LC_ALL, gauntlet_locale)
        except locale.Error:
            pass

    # Set timezone
    tz = os.environ.get("GAUNTLET_TIMEZONE")
    if tz:
        os.environ["TZ"] = tz
        try:
            time.tzset()
        except AttributeError:
            pass  # Windows

    _connected = True


def _patch_time(frozen: datetime.datetime):
    """Monkey-patch datetime and time modules to return frozen values."""
    frozen_epoch = frozen.timestamp()

    # Patch datetime.datetime.now
    _original_now = datetime.datetime.now

    class FrozenDatetime(datetime.datetime):
        @classmethod
        def now(cls, tz=None):
            if tz is not None:
                return frozen.astimezone(tz)
            return frozen

        @classmethod
        def utcnow(cls):
            return frozen

    datetime.datetime = FrozenDatetime

    # Patch time.time
    _original_time = time.time
    time.time = lambda: frozen_epoch

    # Patch time.localtime
    _original_localtime = time.localtime
    time.localtime = lambda secs=None: _original_localtime(frozen_epoch)
