"""@gauntlet.tool decorator — intercepts tool execution for deterministic replay.

When GAUNTLET_ENABLED=1 and GAUNTLET_MODEL_MODE=recorded:
  - Canonicalize args → compute hash → look up fixture
  - Return fixture response WITHOUT calling the wrapped function
  - Record tool call in trace

When not in Gauntlet mode:
  - Call the real function transparently
"""

import functools
import hashlib
import inspect
import json
import os
import time
from typing import Any, Callable, Optional

from gauntlet.events import emit_tool_call, emit_tool_error


# Denylist fields stripped from tool args before hashing.
# For tool args, we use exact-match only (not suffix-based) because
# fields like "order_id" and "item_id" are legitimate tool arguments.
# Suffix-based denylist (_id, _ts, _at, _timestamp) only applies to
# model request canonicalization (HTTP headers/metadata).
DENYLIST_FIELDS = {
    "request_id",
    "timestamp",
    "trace_id",
    "session_id",
}

DENYLIST_PREFIXES = ("metadata.", "extra_headers.")


def _strip_denylist(obj: Any) -> Any:
    """Strip denylisted fields from a dict (recursive)."""
    if isinstance(obj, dict):
        result = {}
        for k, v in sorted(obj.items()):
            if k in DENYLIST_FIELDS:
                continue
            if any(k.startswith(prefix) for prefix in DENYLIST_PREFIXES):
                continue
            result[k] = _strip_denylist(v)
        return result
    elif isinstance(obj, list):
        return [_strip_denylist(item) for item in obj]
    return obj


def _canonicalize_args(tool_name: str, args: dict) -> bytes:
    """Canonicalize tool call args to deterministic JSON bytes."""
    canonical = {
        "tool": tool_name,
        "args": _strip_denylist(args),
    }
    return json.dumps(canonical, sort_keys=True, separators=(",", ":")).encode("utf-8")


def _hash_canonical(data: bytes) -> str:
    """SHA-256 hash of canonical bytes."""
    return hashlib.sha256(data).hexdigest()


def _load_fixture(fixture_hash: str) -> Optional[Any]:
    """Look up a tool fixture by hash from the fixture store."""
    # Check fixture store path from env
    fixture_dir = os.environ.get(
        "GAUNTLET_FIXTURE_DIR", "evals/fixtures/tools"
    )
    fixture_path = os.path.join(fixture_dir, f"{fixture_hash}.json")

    if not os.path.exists(fixture_path):
        return None

    with open(fixture_path, "r") as f:
        data = json.load(f)

    return data.get("response")


def _intercept(tool_name, func, args, call_kwargs):
    """Common interception logic for sync and async wrappers.

    Returns (mode, result, args_dict, canonical_hash, canonical_bytes, start).
    mode is one of: "passthrough", "recorded", "live".
    For "recorded", result contains the fixture response.
    For "passthrough" and "live", result is None.
    """
    gauntlet_enabled = os.environ.get("GAUNTLET_ENABLED") == "1"
    model_mode = os.environ.get("GAUNTLET_MODEL_MODE", "recorded")

    if not gauntlet_enabled or model_mode == "passthrough":
        return "passthrough", None, None, None, None, None

    sig = inspect.signature(func)
    bound = sig.bind(*args, **call_kwargs)
    bound.apply_defaults()
    args_dict = dict(bound.arguments)

    start = time.time()
    canonical_bytes = _canonicalize_args(tool_name, args_dict)
    canonical_hash = _hash_canonical(canonical_bytes)

    if model_mode == "recorded":
        fixture_result = _load_fixture(canonical_hash)
        if fixture_result is None:
            error_msg = (
                f"FIXTURE MISS for tool:{tool_name}\n"
                f"  canonical_hash: {canonical_hash}\n"
                f"  canonical_json: {canonical_bytes.decode()}\n"
                f"  To record: GAUNTLET_MODEL_MODE=live gauntlet record"
            )
            emit_tool_error(tool_name, args_dict, error_msg)
            raise RuntimeError(error_msg)

        duration_ms = int((time.time() - start) * 1000)
        emit_tool_call(
            tool_name=tool_name,
            args=args_dict,
            result=fixture_result,
            fixture_hit=True,
            canonical_hash=canonical_hash,
            duration_ms=duration_ms,
        )
        return "recorded", fixture_result, args_dict, canonical_hash, canonical_bytes, start

    if model_mode == "live":
        return "live", None, args_dict, canonical_hash, canonical_bytes, start

    # Unknown mode — fall through to real function
    return "passthrough", None, None, None, None, None


def _finish_live(tool_name, args_dict, canonical_hash, canonical_bytes, start, result):
    """Post-call logic for live mode: save fixture and emit trace."""
    duration_ms = int((time.time() - start) * 1000)
    _save_fixture(tool_name, canonical_hash, canonical_bytes, result)
    emit_tool_call(
        tool_name=tool_name,
        args=args_dict,
        result=result,
        fixture_hit=False,
        canonical_hash=canonical_hash,
        duration_ms=duration_ms,
    )


def tool(name: Optional[str] = None, **kwargs):
    """Decorator that intercepts tool execution for Gauntlet fixture replay.

    Supports both sync and async functions:
        @gauntlet.tool(name="order_lookup")
        def lookup_order(order_id: str) -> dict: ...

        @gauntlet.tool(name="order_lookup")
        async def lookup_order(order_id: str) -> dict: ...

    When GAUNTLET_ENABLED=1 and GAUNTLET_MODEL_MODE=recorded:
        - The underlying function is NEVER called
        - Fixture response is returned directly

    When not in Gauntlet mode:
        - The real function runs transparently
    """

    def decorator(func: Callable) -> Callable:
        tool_name = name or func.__name__

        if inspect.iscoroutinefunction(func):
            @functools.wraps(func)
            async def async_wrapper(*args, **call_kwargs):
                mode, result, args_dict, canonical_hash, canonical_bytes, start = (
                    _intercept(tool_name, func, args, call_kwargs)
                )
                if mode == "passthrough":
                    return await func(*args, **call_kwargs)
                if mode == "recorded":
                    return result
                # live mode
                result = await func(*args, **call_kwargs)
                _finish_live(tool_name, args_dict, canonical_hash, canonical_bytes, start, result)
                return result

            async_wrapper._gauntlet_tool = True
            async_wrapper._gauntlet_tool_name = tool_name
            async_wrapper._original_func = func
            return async_wrapper
        else:
            @functools.wraps(func)
            def wrapper(*args, **call_kwargs):
                mode, result, args_dict, canonical_hash, canonical_bytes, start = (
                    _intercept(tool_name, func, args, call_kwargs)
                )
                if mode == "passthrough":
                    return func(*args, **call_kwargs)
                if mode == "recorded":
                    return result
                # live mode
                result = func(*args, **call_kwargs)
                _finish_live(tool_name, args_dict, canonical_hash, canonical_bytes, start, result)
                return result

            wrapper._gauntlet_tool = True
            wrapper._gauntlet_tool_name = tool_name
            wrapper._original_func = func
            return wrapper

    return decorator


def _redact_fields(obj: Any, patterns: list) -> Any:
    """Redact fields matching glob patterns like '**.api_key'."""
    if isinstance(obj, dict):
        result = {}
        for k, v in obj.items():
            if any(_field_matches(k, p) for p in patterns):
                result[k] = "[REDACTED]"
            else:
                result[k] = _redact_fields(v, patterns)
        return result
    elif isinstance(obj, list):
        return [_redact_fields(item, patterns) for item in obj]
    return obj


def _field_matches(field_name: str, pattern: str) -> bool:
    """Check if field_name matches a glob pattern like '**.api_key'."""
    if pattern.startswith("**."):
        return field_name == pattern[3:]
    return field_name == pattern


def _save_fixture(
    tool_name: str, fixture_hash: str, canonical_bytes: bytes, result: Any
):
    """Save a tool fixture to the fixture store."""
    fixture_dir = os.environ.get(
        "GAUNTLET_FIXTURE_DIR", "evals/fixtures/tools"
    )
    os.makedirs(fixture_dir, exist_ok=True)

    # Redact sensitive fields before storage
    redact_fields = os.environ.get("GAUNTLET_REDACT_FIELDS", "")
    stored_result = result
    if redact_fields:
        patterns = [p.strip() for p in redact_fields.split(",") if p.strip()]
        stored_result = _redact_fields(result, patterns)

    fixture_path = os.path.join(fixture_dir, f"{fixture_hash}.json")
    fixture_data = {
        "fixture_id": fixture_hash,
        "hash_version": 1,
        "canonical_hash": fixture_hash,
        "tool_name": tool_name,
        "canonical_request": json.loads(canonical_bytes.decode()),
        "response": stored_result,
    }

    with open(fixture_path, "w") as f:
        json.dump(fixture_data, f, indent=2, default=str)
