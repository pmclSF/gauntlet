"""Tests for @gauntlet.tool decorator.

Critical test: verifies that the underlying function is NEVER called
when a fixture is available (Patch 1 requirement).
"""

import json
import hashlib
import os
import tempfile
import pytest

import gauntlet
import gauntlet.decorators as decorators


class TestToolDecorator:
    """Test the @gauntlet.tool decorator intercepts tool execution."""

    def setup_method(self):
        """Set up a temporary fixture directory and enable Gauntlet."""
        decorators._fixture_lock_loaded = False
        decorators._fixture_lock_index = {}
        self.tmpdir = tempfile.mkdtemp()
        self.original_env = {}
        for key in [
            "GAUNTLET_ENABLED",
            "GAUNTLET_MODEL_MODE",
            "GAUNTLET_FIXTURE_DIR",
            "GAUNTLET_REPLAY_LOCKFILE",
            "GAUNTLET_REQUIRE_TOOL_FIXTURE_LOCKFILE",
            "GAUNTLET_REQUIRE_FIXTURE_SIGNATURES",
            "GAUNTLET_TRUSTED_RECORDER_IDENTITIES",
            "GAUNTLET_SUITE",
            "GAUNTLET_SCENARIO_SET_SHA256",
        ]:
            self.original_env[key] = os.environ.get(key)

        os.environ["GAUNTLET_ENABLED"] = "1"
        os.environ["GAUNTLET_MODEL_MODE"] = "recorded"
        os.environ["GAUNTLET_FIXTURE_DIR"] = self.tmpdir

    def teardown_method(self):
        """Restore original environment."""
        for key, value in self.original_env.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value

    def _write_fixture(self, fixture_hash: str, response: dict):
        """Write a fixture file for a given hash."""
        fixture_path = os.path.join(self.tmpdir, f"{fixture_hash}.json")
        fixture_data = {
            "fixture_id": fixture_hash,
            "hash_version": 1,
            "canonical_hash": fixture_hash,
            "tool_name": "test_tool",
            "response": response,
        }
        with open(fixture_path, "w") as f:
            json.dump(fixture_data, f)

    def _write_lockfile(self, entries: list):
        lock_path = os.path.join(self.tmpdir, "replay.lock.json")
        lock_data = {
            "version": 1,
            "generated_at": "2026-03-05T00:00:00Z",
            "fixtures_dir": self.tmpdir,
            "suite": "smoke",
            "scenario_set_sha256": "digest-1",
            "entries": entries,
            "index_sha256": "unused-for-sdk-checks",
        }
        with open(lock_path, "w") as f:
            json.dump(lock_data, f)
        os.environ["GAUNTLET_REPLAY_LOCKFILE"] = lock_path

    def _get_hash_for_tool(self, tool_name: str, args: dict) -> str:
        """Compute the fixture hash for a tool call."""
        from gauntlet.decorators import _canonicalize_args, _hash_canonical

        canonical = _canonicalize_args(tool_name, args)
        return _hash_canonical(canonical)

    def test_underlying_function_never_called_when_fixture_available(self):
        """CRITICAL: The underlying function must NEVER execute in recorded mode
        when a fixture is available. This is the core guarantee of Patch 1."""
        call_count = 0

        @gauntlet.tool(name="order_lookup")
        def lookup_order(order_id: str) -> dict:
            nonlocal call_count
            call_count += 1
            return {"status": "REAL_CALL_SHOULD_NOT_HAPPEN"}

        # Compute hash and write fixture
        fixture_hash = self._get_hash_for_tool(
            "order_lookup", {"order_id": "ord-001"}
        )
        self._write_fixture(
            fixture_hash, {"order_id": "ord-001", "status": "shipped"}
        )

        # Call the decorated function
        result = lookup_order(order_id="ord-001")

        # The underlying function must NOT have been called
        assert call_count == 0, (
            f"Underlying function was called {call_count} time(s) "
            "but should NEVER be called when fixture is available"
        )

        # Result must come from fixture
        assert result == {"order_id": "ord-001", "status": "shipped"}

    def test_fixture_miss_raises_error_in_recorded_mode(self):
        """When no fixture exists in recorded mode, raise RuntimeError."""

        @gauntlet.tool(name="missing_tool")
        def missing_func(x: int) -> dict:
            return {"x": x}

        with pytest.raises(RuntimeError, match="FIXTURE MISS"):
            missing_func(x=42)

    def test_real_function_called_when_gauntlet_disabled(self):
        """When GAUNTLET_ENABLED is not set, the real function runs."""
        os.environ.pop("GAUNTLET_ENABLED", None)

        call_count = 0

        @gauntlet.tool(name="real_tool")
        def real_func(value: str) -> str:
            nonlocal call_count
            call_count += 1
            return f"real:{value}"

        result = real_func(value="hello")
        assert call_count == 1
        assert result == "real:hello"

    def test_real_function_called_in_passthrough_mode(self):
        """In passthrough mode, the real function runs."""
        os.environ["GAUNTLET_MODEL_MODE"] = "passthrough"

        call_count = 0

        @gauntlet.tool(name="passthrough_tool")
        def passthrough_func(n: int) -> int:
            nonlocal call_count
            call_count += 1
            return n * 2

        result = passthrough_func(n=5)
        assert call_count == 1
        assert result == 10

    def test_live_mode_calls_real_function_and_records(self):
        """In live mode, the real function runs and the result is saved."""
        os.environ["GAUNTLET_MODEL_MODE"] = "live"
        os.environ["GAUNTLET_SUITE"] = "smoke"
        os.environ["GAUNTLET_SCENARIO_SET_SHA256"] = "digest-1"

        @gauntlet.tool(name="live_tool")
        def live_func(query: str) -> dict:
            return {"answer": f"response to {query}"}

        result = live_func(query="test")
        assert result == {"answer": "response to test"}

        # Verify fixture was saved
        fixture_hash = self._get_hash_for_tool("live_tool", {"query": "test"})
        fixture_path = os.path.join(self.tmpdir, f"{fixture_hash}.json")
        assert os.path.exists(fixture_path)

        with open(fixture_path) as f:
            saved = json.load(f)
        assert saved["response"] == {"answer": "response to test"}
        assert saved["args"] == {"query": "test"}
        assert saved["suite"] == "smoke"
        assert saved["scenario_set_sha256"] == "digest-1"
        assert saved["provenance"]["recorder_identity"] != ""

    def test_recorded_mode_rejects_fixture_not_indexed_in_lockfile(self):
        os.environ["GAUNTLET_REQUIRE_TOOL_FIXTURE_LOCKFILE"] = "1"
        os.environ["GAUNTLET_SUITE"] = "smoke"
        os.environ["GAUNTLET_SCENARIO_SET_SHA256"] = "digest-1"

        @gauntlet.tool(name="lock_required_tool")
        def lookup(order_id: str) -> dict:
            return {"order_id": order_id}

        fixture_hash = self._get_hash_for_tool(
            "lock_required_tool", {"order_id": "ord-001"}
        )
        self._write_fixture(fixture_hash, {"order_id": "ord-001"})
        self._write_lockfile(entries=[])

        with pytest.raises(RuntimeError, match="not present in replay lockfile"):
            lookup(order_id="ord-001")

    def test_recorded_mode_rejects_fixture_sha_mismatch(self):
        os.environ["GAUNTLET_REQUIRE_TOOL_FIXTURE_LOCKFILE"] = "1"
        os.environ["GAUNTLET_SUITE"] = "smoke"
        os.environ["GAUNTLET_SCENARIO_SET_SHA256"] = "digest-1"

        @gauntlet.tool(name="sha_guard_tool")
        def lookup(order_id: str) -> dict:
            return {"order_id": order_id}

        fixture_hash = self._get_hash_for_tool(
            "sha_guard_tool", {"order_id": "ord-001"}
        )
        self._write_fixture(fixture_hash, {"order_id": "ord-001"})
        fixture_path = os.path.join(self.tmpdir, f"{fixture_hash}.json")
        with open(fixture_path, "rb") as f:
            expected_sha = hashlib.sha256(f.read()).hexdigest()
        self._write_lockfile(
            entries=[
                {
                    "path": f"tools/{fixture_hash}.json",
                    "fixture_type": "tool",
                    "canonical_hash": fixture_hash,
                    "sha256": expected_sha,
                    "size": 0,
                }
            ]
        )
        with open(fixture_path, "w") as f:
            json.dump({"canonical_hash": fixture_hash, "response": {"tampered": True}}, f)

        with pytest.raises(RuntimeError, match="does not match replay lockfile index"):
            lookup(order_id="ord-001")

    def test_denylist_fields_stripped_from_hash(self):
        """Denylisted fields should not affect the fixture hash."""

        @gauntlet.tool(name="denylist_tool")
        def func_with_extra(data: str, request_id: str = "") -> str:
            return data

        hash1 = self._get_hash_for_tool(
            "denylist_tool", {"data": "hello", "request_id": "abc-123"}
        )
        hash2 = self._get_hash_for_tool(
            "denylist_tool", {"data": "hello", "request_id": "xyz-789"}
        )

        assert hash1 == hash2, "Denylisted fields should not affect hash"

    def test_decorator_preserves_metadata(self):
        """The decorator should preserve function metadata for introspection."""

        @gauntlet.tool(name="meta_tool")
        def my_function(x: int) -> int:
            """Docstring preserved."""
            return x

        assert my_function._gauntlet_tool is True
        assert my_function._gauntlet_tool_name == "meta_tool"
        assert my_function.__doc__ == "Docstring preserved."
        assert my_function.__name__ == "my_function"

    def test_default_name_from_function(self):
        """If no name given, use the function name."""

        @gauntlet.tool()
        def auto_named(x: int) -> int:
            return x

        assert auto_named._gauntlet_tool_name == "auto_named"

    def test_multiple_calls_with_different_args(self):
        """Different args should produce different hashes and fixture lookups."""
        call_count = 0

        @gauntlet.tool(name="multi_tool")
        def multi_func(item_id: str) -> dict:
            nonlocal call_count
            call_count += 1
            return {"id": item_id}

        # Write fixtures for two different calls
        hash1 = self._get_hash_for_tool("multi_tool", {"item_id": "A"})
        hash2 = self._get_hash_for_tool("multi_tool", {"item_id": "B"})
        assert hash1 != hash2

        self._write_fixture(hash1, {"id": "A", "status": "active"})
        self._write_fixture(hash2, {"id": "B", "status": "inactive"})

        result_a = multi_func(item_id="A")
        result_b = multi_func(item_id="B")

        assert call_count == 0
        assert result_a == {"id": "A", "status": "active"}
        assert result_b == {"id": "B", "status": "inactive"}


class TestToolDecoratorCanonicalDenylist:
    """Test denylist canonicalization in the tool decorator."""

    def test_exact_match_denylist(self):
        """Exact-match denylisted fields should be stripped from tool args."""
        from gauntlet.decorators import _strip_denylist

        data = {
            "query": "hello",
            "request_id": "abc",
            "timestamp": "2025-01-01",
            "trace_id": "tr-123",
            "session_id": "sess-1",
            "order_id": "ord-001",  # NOT stripped — legitimate tool arg
            "created_at": "2025-01-01",  # NOT stripped — only exact-match for tools
        }

        cleaned = _strip_denylist(data)
        assert "query" in cleaned
        assert "request_id" not in cleaned
        assert "timestamp" not in cleaned
        assert "trace_id" not in cleaned
        assert "session_id" not in cleaned
        # Tool-specific fields preserved (suffix denylist only for model requests)
        assert "order_id" in cleaned
        assert "created_at" in cleaned

    def test_nested_denylist(self):
        """Denylist should apply recursively to nested dicts."""
        from gauntlet.decorators import _strip_denylist

        data = {
            "query": "test",
            "nested": {
                "value": 42,
                "trace_id": "tr-123",
            },
        }

        cleaned = _strip_denylist(data)
        assert cleaned["nested"]["value"] == 42
        assert "trace_id" not in cleaned["nested"]

    def test_unknown_fields_preserved(self):
        """Unknown fields should be preserved (denylist, not allowlist)."""
        from gauntlet.decorators import _strip_denylist

        data = {
            "query": "test",
            "new_sdk_field": "important",
            "custom_param": 42,
        }

        cleaned = _strip_denylist(data)
        assert cleaned["new_sdk_field"] == "important"
        assert cleaned["custom_param"] == 42
