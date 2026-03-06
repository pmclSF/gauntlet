"""OpenAI SDK adapter for Gauntlet.

Provides transport-level instrumentation and deterministic trace metadata
capture for OpenAI SDK calls when Gauntlet is enabled.
"""

import inspect
import os
import time
import warnings
from typing import Any, Callable, Mapping, Optional

from gauntlet.events import emit_event

_INSTRUMENTED_ATTR = "_gauntlet_openai_instrumented"
_PATCHED_INIT_ATTR = "_gauntlet_openai_init_patched"


def _adapter_warnings_enabled() -> bool:
    raw = os.environ.get("GAUNTLET_ADAPTER_WARNINGS", "1").strip().lower()
    return raw not in {"0", "false", "no", "off"}


def _warn_noop(reason: str) -> None:
    if _adapter_warnings_enabled():
        warnings.warn(
            f"Gauntlet OpenAI adapter is running in no-op mode: {reason}",
            RuntimeWarning,
            stacklevel=2,
        )


def _to_plain(value: Any) -> Any:
    if value is None:
        return None
    if hasattr(value, "model_dump") and callable(value.model_dump):
        try:
            return value.model_dump()
        except Exception:
            return str(value)
    if hasattr(value, "dict") and callable(value.dict):
        try:
            return value.dict()
        except Exception:
            return str(value)
    if isinstance(value, (dict, list, str, int, float, bool)):
        return value
    return str(value)


def _extract_model(payload: Any) -> Optional[str]:
    if not isinstance(payload, dict):
        return None
    for key in ("model", "model_name", "deployment", "deployment_id"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return None


def _extract_payload(args: tuple[Any, ...], kwargs: Mapping[str, Any]) -> Any:
    for key in ("json", "body", "payload", "request"):
        value = kwargs.get(key)
        if value is not None:
            return _to_plain(value)
    if len(args) >= 2:
        return _to_plain(args[1])
    return None


def _extract_endpoint(args: tuple[Any, ...], kwargs: Mapping[str, Any]) -> Optional[str]:
    for key in ("path", "url", "endpoint"):
        value = kwargs.get(key)
        if isinstance(value, str) and value.strip():
            return value
    if args:
        head = args[0]
        if isinstance(head, str):
            return head
    return None


def _emit_model_call(
    *,
    provider_family: str,
    model: Optional[str],
    payload: Any,
    response: Any,
    error: Optional[str],
    duration_ms: int,
    endpoint: Optional[str],
    transport: str,
) -> None:
    args: dict[str, Any] = {}
    if endpoint:
        args["endpoint"] = endpoint
    if payload is not None:
        args["request"] = payload
    emit_event(
        "model_call",
        args=args if args else None,
        result=response,
        duration_ms=duration_ms,
        error=error,
        provider_family=provider_family,
        model=model,
        metadata={"transport": transport},
    )


def _wrap_callable(
    callable_obj: Callable[..., Any], provider_family: str, transport: str
) -> Callable[..., Any]:
    if getattr(callable_obj, "_gauntlet_model_instrumented", False):
        return callable_obj

    if inspect.iscoroutinefunction(callable_obj):

        async def async_wrapped(*args: Any, **kwargs: Any) -> Any:
            start = time.time()
            payload = _extract_payload(args, kwargs)
            model = _extract_model(payload)
            endpoint = _extract_endpoint(args, kwargs)
            error = None
            response = None
            try:
                response = await callable_obj(*args, **kwargs)
                return response
            except Exception as exc:
                error = str(exc)
                raise
            finally:
                duration_ms = int((time.time() - start) * 1000)
                _emit_model_call(
                    provider_family=provider_family,
                    model=model,
                    payload=payload,
                    response=_to_plain(response),
                    error=error,
                    duration_ms=duration_ms,
                    endpoint=endpoint,
                    transport=transport,
                )
        wrapped: Callable[..., Any] = async_wrapped

    else:

        def sync_wrapped(*args: Any, **kwargs: Any) -> Any:
            start = time.time()
            payload = _extract_payload(args, kwargs)
            model = _extract_model(payload)
            endpoint = _extract_endpoint(args, kwargs)
            error = None
            response = None
            try:
                response = callable_obj(*args, **kwargs)
                return response
            except Exception as exc:
                error = str(exc)
                raise
            finally:
                duration_ms = int((time.time() - start) * 1000)
                _emit_model_call(
                    provider_family=provider_family,
                    model=model,
                    payload=payload,
                    response=_to_plain(response),
                    error=error,
                    duration_ms=duration_ms,
                    endpoint=endpoint,
                    transport=transport,
                )
        wrapped = sync_wrapped

    setattr(wrapped, "_gauntlet_model_instrumented", True)
    return wrapped


def _instrument_method(target: Any, attr_name: str, provider_family: str, transport: str) -> bool:
    method = getattr(target, attr_name, None)
    if method is None or not callable(method):
        return False
    wrapped = _wrap_callable(method, provider_family, transport)
    try:
        setattr(target, attr_name, wrapped)
    except Exception:
        return False
    return True


def _instrument_resource_create(
    client: Any, path: tuple[str, ...], provider_family: str
) -> bool:
    node = client
    for part in path[:-1]:
        node = getattr(node, part, None)
        if node is None:
            return False
    return _instrument_method(node, path[-1], provider_family, transport="resource")


def _instrument_client(client: Any, provider_family: str = "openai") -> bool:
    if client is None:
        return False
    if getattr(client, _INSTRUMENTED_ATTR, False):
        return True

    patched = False
    for attr in ("request", "_request", "post"):
        patched = _instrument_method(
            client, attr, provider_family, transport="transport"
        ) or patched

    for path in (
        ("chat", "completions", "create"),
        ("responses", "create"),
        ("completions", "create"),
    ):
        patched = _instrument_resource_create(client, path, provider_family) or patched

    if patched:
        setattr(client, _INSTRUMENTED_ATTR, True)
    else:
        _warn_noop("could not find OpenAI transport/resource methods to patch")
    return patched


def _patch_client_constructor(cls: Any, provider_family: str = "openai") -> bool:
    if cls is None:
        return False
    if getattr(cls, _PATCHED_INIT_ATTR, False):
        return True

    original_init = cls.__init__

    def patched_init(self: Any, *args: Any, **kwargs: Any) -> None:
        original_init(self, *args, **kwargs)
        _instrument_client(self, provider_family=provider_family)

    try:
        cls.__init__ = patched_init
    except Exception:
        return False
    setattr(cls, _PATCHED_INIT_ATTR, True)
    return True


def install_openai_instrumentation() -> dict[str, Any]:
    """Install constructor hooks so OpenAI clients are auto-instrumented."""
    if os.environ.get("GAUNTLET_ENABLED") != "1":
        return {"enabled": False, "patched": False, "reason": "gauntlet_disabled"}
    try:
        import openai
    except ImportError:
        _warn_noop("openai package not installed")
        return {"enabled": True, "patched": False, "reason": "openai_not_installed"}

    patched = False
    for cls_name in ("OpenAI", "AsyncOpenAI", "Client", "AsyncClient"):
        cls = getattr(openai, cls_name, None)
        patched = _patch_client_constructor(cls, provider_family="openai") or patched
    if not patched:
        _warn_noop("OpenAI SDK classes were not found")
    return {"enabled": True, "patched": patched}


def patch_openai_client(client: Any = None) -> Any:
    """Patch an OpenAI client for transport-level tracing and metadata capture."""
    if os.environ.get("GAUNTLET_ENABLED") != "1":
        return client

    if client is None:
        try:
            import openai

            client = openai.OpenAI()
        except ImportError:
            raise ImportError("openai package required: pip install openai")

    _instrument_client(client, provider_family="openai")
    return client


def get_model_from_env() -> Optional[str]:
    """Get the model override from Gauntlet env, if set."""
    return os.environ.get("GAUNTLET_MODEL_OVERRIDE")
