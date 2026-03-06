"""Integration tests for adapter interception using local mock services.

These tests exercise real OpenAI/Anthropic SDK clients against a local HTTP
server and verify Gauntlet model-call interception behavior.
"""

from __future__ import annotations

import importlib
import json
import threading
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Iterator

import pytest


class _MockLLMHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_POST(self) -> None:  # noqa: N802
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length > 0 else b"{}"
        payload = json.loads(body.decode("utf-8"))
        self.server.requests.append({"path": self.path, "payload": payload})  # type: ignore[attr-defined]

        if self.path.endswith("/chat/completions"):
            response = {
                "id": "chatcmpl-local",
                "object": "chat.completion",
                "created": 1,
                "model": payload.get("model", "gpt-4o-mini"),
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": "mock-openai"},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": 8,
                    "completion_tokens": 2,
                    "total_tokens": 10,
                },
            }
            self._write_json(200, response)
            return

        if self.path.endswith("/messages"):
            response = {
                "id": "msg_local_1",
                "type": "message",
                "role": "assistant",
                "model": payload.get("model", "claude-3-5-sonnet-latest"),
                "content": [{"type": "text", "text": "mock-anthropic"}],
                "stop_reason": "end_turn",
                "stop_sequence": None,
                "usage": {"input_tokens": 9, "output_tokens": 3},
            }
            self._write_json(200, response)
            return

        self._write_json(404, {"error": "not found", "path": self.path})

    def _write_json(self, status: int, payload: dict[str, Any]) -> None:
        encoded = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def log_message(self, format: str, *args: Any) -> None:  # noqa: A003
        # Keep test output deterministic and quiet.
        return


@contextmanager
def _mock_server() -> Iterator[str]:
    try:
        server = ThreadingHTTPServer(("127.0.0.1", 0), _MockLLMHandler)
    except PermissionError as exc:
        pytest.skip(f"local socket bind not permitted in this environment: {exc}")
    server.requests = []  # type: ignore[attr-defined]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        host, port = server.server_address
        yield f"http://{host}:{port}"
    finally:
        server.shutdown()
        thread.join(timeout=2)
        server.server_close()


def _reload(module_name: str) -> Any:
    module = importlib.import_module(module_name)
    return importlib.reload(module)


def test_openai_adapter_intercepts_real_chat_completions(monkeypatch: pytest.MonkeyPatch) -> None:
    openai = pytest.importorskip("openai")
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")

    adapter = _reload("gauntlet.adapters.openai")
    captured: list[tuple[str, dict[str, Any]]] = []
    monkeypatch.setattr(
        adapter, "emit_event", lambda event_type, **kwargs: captured.append((event_type, kwargs))
    )

    with _mock_server() as base_url:
        client = openai.OpenAI(api_key="test-key", base_url=f"{base_url}/v1")
        patched = adapter.patch_openai_client(client)
        response = patched.chat.completions.create(
            model="gpt-4o-mini",
            messages=[{"role": "user", "content": "hello"}],
            tools=[
                {
                    "type": "function",
                    "function": {
                        "name": "order_lookup",
                        "description": "Lookup order",
                        "parameters": {"type": "object", "properties": {}},
                    },
                }
            ],
        )

    assert response.choices[0].message.content == "mock-openai"
    model_events = [payload for event, payload in captured if event == "model_call"]
    assert model_events, "expected at least one model_call event"
    assert model_events[-1]["provider_family"] == "openai"
    assert model_events[-1]["model"] == "gpt-4o-mini"


def test_anthropic_adapter_intercepts_real_messages_create(monkeypatch: pytest.MonkeyPatch) -> None:
    anthropic = pytest.importorskip("anthropic")
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")

    adapter = _reload("gauntlet.adapters.anthropic")
    captured: list[tuple[str, dict[str, Any]]] = []
    monkeypatch.setattr(
        adapter, "emit_event", lambda event_type, **kwargs: captured.append((event_type, kwargs))
    )

    with _mock_server() as base_url:
        client = anthropic.Anthropic(api_key="test-key", base_url=f"{base_url}/v1")
        patched = adapter.patch_anthropic_client(client)
        response = patched.messages.create(
            model="claude-3-5-sonnet-latest",
            max_tokens=32,
            messages=[{"role": "user", "content": "hello"}],
            tools=[
                {
                    "name": "order_lookup",
                    "description": "Lookup order",
                    "input_schema": {"type": "object", "properties": {}},
                }
            ],
        )

    assert response.content[0].text == "mock-anthropic"
    model_events = [payload for event, payload in captured if event == "model_call"]
    assert model_events, "expected at least one model_call event"
    assert model_events[-1]["provider_family"] == "anthropic"
    assert model_events[-1]["model"] == "claude-3-5-sonnet-latest"


def test_langchain_adapter_emits_events_with_real_callback_class(monkeypatch: pytest.MonkeyPatch) -> None:
    pytest.importorskip("langchain_core")
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")

    adapter = _reload("gauntlet.adapters.langchain")
    captured: list[tuple[str, dict[str, Any]]] = []
    monkeypatch.setattr(
        adapter, "emit_event", lambda event_type, **kwargs: captured.append((event_type, kwargs))
    )

    callbacks = adapter.get_gauntlet_callbacks()
    assert callbacks, "expected real langchain callback handler"
    cb = callbacks[0]

    cb.on_tool_start({"name": "order_lookup"}, '{"order_id":"ord-1"}', run_id="tool-1")
    cb.on_tool_end({"status": "ok"}, run_id="tool-1")
    cb.on_llm_start(
        {"kwargs": {"model": "gpt-4o-mini"}},
        ["hello"],
        run_id="llm-1",
        invocation_params={"model": "gpt-4o-mini"},
    )
    cb.on_llm_end({"text": "done"}, run_id="llm-1")

    event_types = [event for event, _ in captured]
    assert "tool_call" in event_types
    assert "model_call" in event_types
