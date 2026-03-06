"""LangChain adapter for Gauntlet.

Provides callback-based model/tool event instrumentation for LangChain
workloads.
"""

import json
import os
import time
import warnings
from typing import Any, Mapping, Optional

from gauntlet.events import emit_event


def _adapter_warnings_enabled() -> bool:
    raw = os.environ.get("GAUNTLET_ADAPTER_WARNINGS", "1").strip().lower()
    return raw not in {"0", "false", "no", "off"}


def _warn_noop(reason: str) -> None:
    if _adapter_warnings_enabled():
        warnings.warn(
            f"Gauntlet LangChain adapter is running in no-op mode: {reason}",
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


def _decode_tool_input(value: Any) -> Any:
    if isinstance(value, str):
        try:
            return json.loads(value)
        except Exception:
            return {"input": value}
    if isinstance(value, dict):
        return value
    return _to_plain(value)


def _extract_model_name(serialized: Any, kwargs: Mapping[str, Any]) -> Optional[str]:
    invocation_params = kwargs.get("invocation_params")
    if isinstance(invocation_params, dict):
        for key in ("model", "model_name"):
            value = invocation_params.get(key)
            if isinstance(value, str) and value.strip():
                return value.strip()
    if isinstance(serialized, dict):
        kwargs_block = serialized.get("kwargs")
        if isinstance(kwargs_block, dict):
            for key in ("model", "model_name"):
                value = kwargs_block.get(key)
                if isinstance(value, str) and value.strip():
                    return value.strip()
    return None


def patch_langchain_llm(llm: Any = None) -> Any:
    """Attach Gauntlet callbacks to a LangChain LLM/runnable when possible."""
    if os.environ.get("GAUNTLET_ENABLED") != "1":
        return llm

    callbacks = get_gauntlet_callbacks()
    if not callbacks:
        _warn_noop("langchain-core callback APIs are unavailable")
        return llm
    if llm is None:
        return llm

    # Common LangChain integration styles:
    # 1) runnable.callbacks list
    # 2) callback_manager.add_handler(...)
    # 3) callback_manager.handlers list
    if hasattr(llm, "callbacks"):
        current = getattr(llm, "callbacks", None)
        if current is None:
            current = []
        for cb in callbacks:
            if all(type(existing) is not type(cb) for existing in current):
                current.append(cb)
        setattr(llm, "callbacks", current)
        return llm

    callback_manager = getattr(llm, "callback_manager", None)
    if callback_manager is None:
        _warn_noop("target object has no callbacks or callback_manager")
        return llm

    add_handler = getattr(callback_manager, "add_handler", None)
    if callable(add_handler):
        for cb in callbacks:
            try:
                add_handler(cb, True)
            except TypeError:
                add_handler(cb)
        return llm

    handlers = getattr(callback_manager, "handlers", None)
    if isinstance(handlers, list):
        for cb in callbacks:
            if all(type(existing) is not type(cb) for existing in handlers):
                handlers.append(cb)
        return llm

    _warn_noop("callback manager does not expose add_handler/handlers")
    return llm


def install_langchain_instrumentation() -> dict[str, Any]:
    """Validate LangChain callback availability for runtime instrumentation."""
    if os.environ.get("GAUNTLET_ENABLED") != "1":
        return {"enabled": False, "patched": False, "reason": "gauntlet_disabled"}
    callbacks = get_gauntlet_callbacks()
    if callbacks:
        return {"enabled": True, "patched": True}
    _warn_noop("langchain-core callback APIs are unavailable")
    return {"enabled": True, "patched": False, "reason": "langchain_not_installed"}


def get_gauntlet_callbacks() -> list[Any]:
    """Return LangChain callback handlers for Gauntlet tracing."""
    if os.environ.get("GAUNTLET_ENABLED") != "1":
        return []

    try:
        from langchain_core.callbacks import BaseCallbackHandler
    except ImportError:
        return []

    class GauntletTraceCallback(BaseCallbackHandler):
        """Records tool and model calls for Gauntlet traces."""

        def __init__(self) -> None:
            super().__init__()
            self._tool_runs: dict[str, dict[str, Any]] = {}
            self._llm_runs: dict[str, dict[str, Any]] = {}

        def on_tool_start(self, serialized: Any, input_str: Any, **kwargs: Any) -> None:
            run_id = str(kwargs.get("run_id") or "")
            self._tool_runs[run_id] = {
                "start": time.time(),
                "serialized": _to_plain(serialized),
                "args": _decode_tool_input(input_str),
            }

        def on_tool_end(self, output: Any, **kwargs: Any) -> None:
            run_id = str(kwargs.get("run_id") or "")
            state = self._tool_runs.pop(run_id, {})
            start = state.get("start", time.time())
            serialized = state.get("serialized", {})
            args = state.get("args")
            tool_name = None
            if isinstance(serialized, dict):
                tool_name = serialized.get("name")
                if not tool_name and isinstance(serialized.get("id"), list):
                    tool_name = ".".join(str(x) for x in serialized["id"])
            duration_ms = int((time.time() - start) * 1000)
            emit_event(
                "tool_call",
                tool_name=tool_name,
                args=args if isinstance(args, dict) else {"input": args},
                result=_to_plain(output),
                duration_ms=duration_ms,
                metadata={"framework": "langchain"},
            )

        def on_tool_error(self, error: Any, **kwargs: Any) -> None:
            run_id = str(kwargs.get("run_id") or "")
            state = self._tool_runs.pop(run_id, {})
            serialized = state.get("serialized", {})
            tool_name = None
            if isinstance(serialized, dict):
                tool_name = serialized.get("name")
            emit_event(
                "tool_error",
                tool_name=tool_name,
                args=state.get("args") if isinstance(state.get("args"), dict) else None,
                error=str(error),
                metadata={"framework": "langchain"},
            )

        def on_llm_start(self, serialized: Any, prompts: Any, **kwargs: Any) -> None:
            run_id = str(kwargs.get("run_id") or "")
            self._llm_runs[run_id] = {
                "start": time.time(),
                "serialized": _to_plain(serialized),
                "prompts": _to_plain(prompts),
                "model": _extract_model_name(serialized, kwargs),
            }

        def on_llm_end(self, response: Any, **kwargs: Any) -> None:
            run_id = str(kwargs.get("run_id") or "")
            state = self._llm_runs.pop(run_id, {})
            start = state.get("start", time.time())
            duration_ms = int((time.time() - start) * 1000)
            emit_event(
                "model_call",
                args={"prompts": state.get("prompts")},
                result=_to_plain(response),
                duration_ms=duration_ms,
                provider_family="langchain",
                model=state.get("model"),
                metadata={"framework": "langchain"},
            )

        def on_llm_error(self, error: Any, **kwargs: Any) -> None:
            run_id = str(kwargs.get("run_id") or "")
            state = self._llm_runs.pop(run_id, {})
            start = state.get("start", time.time())
            duration_ms = int((time.time() - start) * 1000)
            emit_event(
                "model_call",
                args={"prompts": state.get("prompts")},
                duration_ms=duration_ms,
                error=str(error),
                provider_family="langchain",
                model=state.get("model"),
                metadata={"framework": "langchain"},
            )

    return [GauntletTraceCallback()]
