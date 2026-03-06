"""Structured event emitters for Gauntlet traces."""

import json
import os
import sys
import time
from typing import Any, Optional


def _write_trace_line(line: str) -> None:
    """Write a trace line to the trace file if set, else stdout."""
    trace_path = os.environ.get("GAUNTLET_TRACE_FILE")
    if trace_path:
        with open(trace_path, "a", encoding="utf-8") as f:
            f.write(line + "\n")
            f.flush()
    else:
        sys.stdout.write(line + "\n")
        sys.stdout.flush()


def emit_event(
    event_type: str,
    tool_name: Optional[str] = None,
    args: Optional[dict[str, Any]] = None,
    result: Optional[Any] = None,
    fixture_hit: bool = False,
    canonical_hash: Optional[str] = None,
    duration_ms: int = 0,
    error: Optional[str] = None,
    provider_family: Optional[str] = None,
    model: Optional[str] = None,
    metadata: Optional[dict[str, Any]] = None,
) -> None:
    """Emit a structured trace event."""
    event = {
        "gauntlet_event": True,
        "type": event_type,
        "timestamp": time.time(),
    }
    if tool_name is not None:
        event["tool_name"] = tool_name
    if args is not None:
        event["args"] = args
    if result is not None:
        event["result"] = result
    if fixture_hit:
        event["fixture_hit"] = True
    if canonical_hash is not None:
        event["canonical_hash"] = canonical_hash
    if duration_ms > 0:
        event["duration_ms"] = duration_ms
    if error is not None:
        event["error"] = error
    if provider_family is not None:
        event["provider_family"] = provider_family
    if model is not None:
        event["model"] = model
    if metadata is not None:
        event["metadata"] = metadata

    line = json.dumps(event, separators=(",", ":"), default=str)
    _write_trace_line(line)


def emit_tool_call(
    tool_name: str,
    args: dict[str, Any],
    result: Any,
    fixture_hit: bool,
    canonical_hash: str,
    duration_ms: int,
) -> None:
    emit_event(
        "tool_call",
        tool_name=tool_name,
        args=args,
        result=result,
        fixture_hit=fixture_hit,
        canonical_hash=canonical_hash,
        duration_ms=duration_ms,
    )


def emit_tool_error(tool_name: str, args: dict[str, Any], error: str) -> None:
    emit_event("tool_error", tool_name=tool_name, args=args, error=error)
