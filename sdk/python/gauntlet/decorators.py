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
import sys
import time
from typing import Any, Callable, Optional
from datetime import datetime, timezone

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

_fixture_lock_loaded = False
_fixture_lock_index = {}


def _env_flag(name: str) -> bool:
    return os.environ.get(name, "").strip().lower() in {
        "1",
        "true",
        "yes",
        "on",
    }


def _fixtures_base_dir() -> str:
    fixture_dir = os.environ.get(
        "GAUNTLET_FIXTURE_DIR", "evals/fixtures/tools"
    )
    return os.path.dirname(os.path.abspath(fixture_dir))


def _replay_lockfile_path() -> str:
    override = os.environ.get("GAUNTLET_REPLAY_LOCKFILE", "").strip()
    if override:
        return override
    return os.path.join(_fixtures_base_dir(), "replay.lock.json")


def _expected_suite() -> str:
    return os.environ.get("GAUNTLET_SUITE", "").strip()


def _expected_scenario_set_sha256() -> str:
    return os.environ.get("GAUNTLET_SCENARIO_SET_SHA256", "").strip()


def _required_tool_lockfile() -> bool:
    return _env_flag("GAUNTLET_REQUIRE_TOOL_FIXTURE_LOCKFILE")


def _required_fixture_signatures() -> bool:
    return _env_flag("GAUNTLET_REQUIRE_FIXTURE_SIGNATURES")


def _trusted_recorder_identities() -> set[str]:
    raw = os.environ.get(
        "GAUNTLET_TRUSTED_RECORDER_IDENTITIES", ""
    ).strip()
    if not raw:
        return set()
    return {part.strip().lower() for part in raw.split(",") if part.strip()}


def _load_tool_lock_index() -> dict:
    global _fixture_lock_loaded, _fixture_lock_index
    if _fixture_lock_loaded:
        return _fixture_lock_index

    _fixture_lock_loaded = True
    _fixture_lock_index = {}
    lockfile_path = _replay_lockfile_path()
    if not os.path.exists(lockfile_path):
        if _required_tool_lockfile():
            raise RuntimeError(
                "tool fixture replay requires replay lockfile but file is "
                f"missing: {lockfile_path}"
            )
        return _fixture_lock_index

    with open(lockfile_path, "r") as f:
        lockfile = json.load(f)
    if not isinstance(lockfile, dict):
        raise RuntimeError(
            f"invalid replay lockfile format: {lockfile_path}"
        )

    expected_suite = _expected_suite()
    lock_suite = str(lockfile.get("suite", "")).strip()
    if expected_suite and lock_suite and lock_suite != expected_suite:
        raise RuntimeError(
            "replay lockfile suite mismatch for tool replay: "
            f"lockfile={lock_suite} expected={expected_suite}"
        )
    expected_digest = _expected_scenario_set_sha256()
    lock_digest = str(lockfile.get("scenario_set_sha256", "")).strip()
    if expected_digest and lock_digest and lock_digest != expected_digest:
        raise RuntimeError(
            "replay lockfile scenario_set_sha256 mismatch for tool replay: "
            f"lockfile={lock_digest} expected={expected_digest}"
        )

    entries = lockfile.get("entries", [])
    if not isinstance(entries, list):
        raise RuntimeError(
            f"invalid replay lockfile entries format: {lockfile_path}"
        )
    for entry in entries:
        if not isinstance(entry, dict):
            continue
        if str(entry.get("fixture_type", "")).strip() != "tool":
            continue
        canonical_hash = str(entry.get("canonical_hash", "")).strip().lower()
        sha256 = str(entry.get("sha256", "")).strip().lower()
        if not canonical_hash or not sha256:
            continue
        prev = _fixture_lock_index.get(canonical_hash)
        if prev and prev != sha256:
            raise RuntimeError(
                "replay lockfile has conflicting sha256 values for tool "
                f"fixture hash {canonical_hash}"
            )
        _fixture_lock_index[canonical_hash] = sha256
    return _fixture_lock_index


def _validate_fixture_signature_presence(fixture_path: str, fixture_data: dict):
    if not _required_fixture_signatures():
        return
    signature = fixture_data.get("signature")
    if not isinstance(signature, dict):
        raise RuntimeError(
            f"tool fixture {fixture_path} missing signature metadata"
        )
    for field in ("algorithm", "key_fingerprint", "payload_sha256", "value"):
        value = str(signature.get(field, "")).strip()
        if not value:
            raise RuntimeError(
                f"tool fixture {fixture_path} signature missing {field}"
            )

    trusted = _trusted_recorder_identities()
    if not trusted:
        return
    provenance = fixture_data.get("provenance")
    provenance_id = ""
    if isinstance(provenance, dict):
        provenance_id = str(
            provenance.get("recorder_identity", "")
        ).strip().lower()
    signer_id = str(signature.get("signer_identity", "")).strip().lower()
    effective = signer_id or provenance_id
    if not effective:
        raise RuntimeError(
            f"tool fixture {fixture_path} missing recorder identity"
        )
    if effective not in trusted:
        raise RuntimeError(
            f"tool fixture {fixture_path} recorder identity "
            f"'{effective}' is not trusted"
        )


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
    lock_index = _load_tool_lock_index()
    expected_sha = lock_index.get(fixture_hash.lower())
    if _required_tool_lockfile() and not expected_sha:
        raise RuntimeError(
            "FIXTURE TRUST FAILURE for tool replay\n"
            f"  canonical_hash: {fixture_hash}\n"
            "  Fixture hash not present in replay lockfile index"
        )

    # Check fixture store path from env
    fixture_dir = os.environ.get(
        "GAUNTLET_FIXTURE_DIR", "evals/fixtures/tools"
    )
    fixture_path = os.path.join(fixture_dir, f"{fixture_hash}.json")

    if not os.path.exists(fixture_path):
        return None

    with open(fixture_path, "rb") as f:
        raw = f.read()
    fixture_sha256 = hashlib.sha256(raw).hexdigest()
    if expected_sha and fixture_sha256.lower() != expected_sha.lower():
        raise RuntimeError(
            "FIXTURE TRUST FAILURE for tool replay\n"
            f"  canonical_hash: {fixture_hash}\n"
            f"  fixture_path: {fixture_path}\n"
            "  Fixture SHA-256 does not match replay lockfile index"
        )

    data = json.loads(raw.decode("utf-8"))
    if not isinstance(data, dict):
        raise RuntimeError(f"invalid tool fixture format: {fixture_path}")

    fixture_canonical_hash = str(data.get("canonical_hash", "")).strip()
    if fixture_canonical_hash and fixture_canonical_hash != fixture_hash:
        raise RuntimeError(
            "tool fixture canonical_hash mismatch for replay\n"
            f"  fixture_path: {fixture_path}\n"
            f"  requested: {fixture_hash}\n"
            f"  fixture: {fixture_canonical_hash}"
        )

    expected_suite = _expected_suite()
    fixture_suite = str(data.get("suite", "")).strip()
    if expected_suite and fixture_suite and fixture_suite != expected_suite:
        raise RuntimeError(
            "tool fixture suite mismatch for replay\n"
            f"  fixture_path: {fixture_path}\n"
            f"  expected: {expected_suite}\n"
            f"  fixture: {fixture_suite}"
        )
    expected_digest = _expected_scenario_set_sha256()
    fixture_digest = str(data.get("scenario_set_sha256", "")).strip()
    if expected_digest and fixture_digest and fixture_digest != expected_digest:
        raise RuntimeError(
            "tool fixture scenario_set_sha256 mismatch for replay\n"
            f"  fixture_path: {fixture_path}\n"
            f"  expected: {expected_digest}\n"
            f"  fixture: {fixture_digest}"
        )

    _validate_fixture_signature_presence(fixture_path, data)

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


def _finish_live(
    tool_name, args_dict, canonical_hash, canonical_bytes, start, result
):
    """Post-call logic for live mode: save fixture and emit trace."""
    duration_ms = int((time.time() - start) * 1000)
    _save_fixture(
        tool_name, args_dict, canonical_hash, canonical_bytes, result
    )
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
    tool_name: str,
    args_dict: dict,
    fixture_hash: str,
    canonical_bytes: bytes,
    result: Any,
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
        "args_hash": fixture_hash,
        "args": args_dict,
        "canonical_request": json.loads(canonical_bytes.decode()),
        "response": stored_result,
        "recorded_at": datetime.now(timezone.utc)
        .isoformat()
        .replace("+00:00", "Z"),
        "provenance": _build_provenance("sdk_tool_live"),
    }
    suite = _expected_suite()
    if suite:
        fixture_data["suite"] = suite
    scenario_digest = _expected_scenario_set_sha256()
    if scenario_digest:
        fixture_data["scenario_set_sha256"] = scenario_digest

    with open(fixture_path, "w") as f:
        json.dump(fixture_data, f, indent=2, default=str, sort_keys=True)


def _first_non_empty(*values: str) -> str:
    for value in values:
        if value and value.strip():
            return value.strip()
    return ""


def _build_provenance(source: str) -> dict:
    identity = _first_non_empty(
        os.environ.get("GAUNTLET_RECORDER_IDENTITY", ""),
        os.environ.get("GITHUB_ACTOR", ""),
        os.environ.get("USER", ""),
        os.environ.get("USERNAME", ""),
        "unknown",
    )
    commit = _first_non_empty(
        os.environ.get("GAUNTLET_COMMIT_SHA", ""),
        os.environ.get("GITHUB_SHA", ""),
        "unknown",
    )
    return {
        "commit_sha": commit,
        "recorder_identity": identity,
        "toolchain_versions": {
            "python": f"{sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}",
        },
        "sdk_versions": {"gauntlet_python_sdk": "0.1.0"},
        "source": source,
    }
