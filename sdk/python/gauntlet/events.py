"""Structured event emitters for Gauntlet traces."""

import json
import sys
import time
from typing import Any, Optional


def emit_event(
    event_type: str,
    tool_name: Optional[str] = None,
    args: Optional[dict] = None,
    result: Optional[Any] = None,
    fixture_hit: bool = False,
    canonical_hash: Optional[str] = None,
    duration_ms: int = 0,
    error: Optional[str] = None,
):
    """Emit a structured trace event to stdout (JSON-lines protocol)."""
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

    line = json.dumps(event, separators=(",", ":"), default=str)
    sys.stdout.write(line + "\n")
    sys.stdout.flush()


def emit_tool_call(
    tool_name: str,
    args: dict,
    result: Any,
    fixture_hit: bool,
    canonical_hash: str,
    duration_ms: int,
):
    emit_event(
        "tool_call",
        tool_name=tool_name,
        args=args,
        result=result,
        fixture_hit=fixture_hit,
        canonical_hash=canonical_hash,
        duration_ms=duration_ms,
    )


def emit_tool_error(tool_name: str, args: dict, error: str):
    emit_event("tool_error", tool_name=tool_name, args=args, error=error)
