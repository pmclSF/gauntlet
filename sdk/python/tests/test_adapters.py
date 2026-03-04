import importlib
import sys
import types


def _reload(module_name):
    module = importlib.import_module(module_name)
    return importlib.reload(module)


def test_openai_patch_client_emits_model_call(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    adapter = _reload("gauntlet.adapters.openai")

    captured = []
    monkeypatch.setattr(
        adapter, "emit_event", lambda event_type, **kwargs: captured.append((event_type, kwargs))
    )

    class FakeClient:
        def request(self, path, json=None, **kwargs):
            return {"ok": True, "path": path, "json": json}

    client = FakeClient()
    patched = adapter.patch_openai_client(client)
    out = patched.request("/v1/chat/completions", json={"model": "gpt-4o"})
    assert out["ok"] is True
    assert captured
    event_type, payload = captured[0]
    assert event_type == "model_call"
    assert payload["provider_family"] == "openai"
    assert payload["model"] == "gpt-4o"
    assert payload["metadata"]["transport"] in {"transport", "resource"}


def test_anthropic_patch_client_emits_model_call(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    adapter = _reload("gauntlet.adapters.anthropic")

    captured = []
    monkeypatch.setattr(
        adapter, "emit_event", lambda event_type, **kwargs: captured.append((event_type, kwargs))
    )

    class FakeMessages:
        def create(self, body=None, **kwargs):
            return {"id": "msg_1", "body": body}

    class FakeClient:
        def __init__(self):
            self.messages = FakeMessages()

    client = FakeClient()
    patched = adapter.patch_anthropic_client(client)
    out = patched.messages.create(body={"model": "claude-3-5-sonnet"})
    assert out["id"] == "msg_1"
    assert captured
    event_type, payload = captured[0]
    assert event_type == "model_call"
    assert payload["provider_family"] == "anthropic"
    assert payload["model"] == "claude-3-5-sonnet"
    assert payload["metadata"]["transport"] == "resource"


def test_langchain_callbacks_emit_tool_and_model_events(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    adapter = _reload("gauntlet.adapters.langchain")

    fake_pkg = types.ModuleType("langchain_core")
    fake_callbacks = types.ModuleType("langchain_core.callbacks")

    class BaseCallbackHandler:
        pass

    fake_callbacks.BaseCallbackHandler = BaseCallbackHandler
    monkeypatch.setitem(sys.modules, "langchain_core", fake_pkg)
    monkeypatch.setitem(sys.modules, "langchain_core.callbacks", fake_callbacks)

    captured = []
    monkeypatch.setattr(
        adapter, "emit_event", lambda event_type, **kwargs: captured.append((event_type, kwargs))
    )

    callbacks = adapter.get_gauntlet_callbacks()
    assert len(callbacks) == 1
    cb = callbacks[0]

    cb.on_tool_start({"name": "order_lookup"}, '{"order_id":"ord-1"}', run_id="tool-1")
    cb.on_tool_end({"status": "ok"}, run_id="tool-1")
    cb.on_llm_start(
        {"kwargs": {"model": "gpt-4o-mini"}},
        ["hello"],
        run_id="llm-1",
        invocation_params={"model": "gpt-4o-mini"},
    )
    cb.on_llm_end({"text": "hi"}, run_id="llm-1")

    types_seen = [event for event, _ in captured]
    assert "tool_call" in types_seen
    assert "model_call" in types_seen

    model_event = [payload for event, payload in captured if event == "model_call"][-1]
    assert model_event["provider_family"] == "langchain"
    assert model_event["model"] == "gpt-4o-mini"


def test_patch_langchain_llm_appends_callback(monkeypatch):
    monkeypatch.setenv("GAUNTLET_ENABLED", "1")
    adapter = _reload("gauntlet.adapters.langchain")

    fake_pkg = types.ModuleType("langchain_core")
    fake_callbacks = types.ModuleType("langchain_core.callbacks")

    class BaseCallbackHandler:
        pass

    fake_callbacks.BaseCallbackHandler = BaseCallbackHandler
    monkeypatch.setitem(sys.modules, "langchain_core", fake_pkg)
    monkeypatch.setitem(sys.modules, "langchain_core.callbacks", fake_callbacks)

    class FakeLLM:
        def __init__(self):
            self.callbacks = []

    llm = FakeLLM()
    patched = adapter.patch_langchain_llm(llm)
    assert patched is llm
    assert len(llm.callbacks) == 1
