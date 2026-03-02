#!/usr/bin/env bash
set -euo pipefail

# Integration test for the Gauntlet support-agent example.
# Verifies that the agent runs correctly under Gauntlet fixture replay.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
AGENT_DIR="$(dirname "$SCRIPT_DIR")"
ROOT_DIR="$(dirname "$(dirname "$AGENT_DIR")")"
BINARY="$ROOT_DIR/bin/gauntlet"

echo "=== Gauntlet Integration Test ==="
echo "Agent dir: $AGENT_DIR"
echo "Binary: $BINARY"

# Verify binary exists
if [ ! -f "$BINARY" ]; then
    echo "ERROR: gauntlet binary not found at $BINARY"
    echo "Run 'make build' first"
    exit 1
fi

# Verify Python SDK is importable
echo "Checking Python SDK..."
cd "$AGENT_DIR"
python3 -c "import gauntlet; print(f'SDK version: {gauntlet.__version__}')" || {
    echo "ERROR: Failed to import gauntlet SDK"
    echo "Run: pip install -e $ROOT_DIR/sdk/python/"
    exit 1
}

# Run the gauntlet suite in dry-run mode first
echo ""
echo "=== Dry Run ==="
"$BINARY" run --suite smoke --config evals/gauntlet.yml --dry-run || {
    echo "Dry run exited with code $?"
}

echo ""
echo "=== Scenario Validation ==="

# Validate each scenario file parses correctly
for scenario in evals/smoke/*.yaml; do
    name=$(basename "$scenario" .yaml)
    echo "  Validating: $name"
    python3 -c "
import yaml, sys
with open('$scenario') as f:
    data = yaml.safe_load(f)
assert 'scenario' in data, 'Missing scenario field'
assert 'input' in data, 'Missing input field'
assert 'assertions' in data, 'Missing assertions field'
print(f'    OK: {data[\"scenario\"]} ({len(data[\"assertions\"])} assertions)')
" || {
        echo "  FAIL: $scenario"
        exit 1
    }
done

# Validate fixture files
echo ""
echo "=== Fixture Validation ==="
fixture_count=$(find evals/fixtures -name "*.json" 2>/dev/null | wc -l | tr -d ' ')
echo "  Found $fixture_count fixture files"

for fixture in evals/fixtures/tools/*.json; do
    python3 -c "
import json, sys
with open('$fixture') as f:
    data = json.load(f)
assert 'fixture_id' in data, 'Missing fixture_id'
assert 'canonical_hash' in data, 'Missing canonical_hash'
assert 'response' in data, 'Missing response'
print(f'    OK: {data[\"tool_name\"]} ({data[\"fixture_id\"][:12]}...)')
" || {
        echo "  FAIL: $fixture"
        exit 1
    }
done

# Validate baselines
echo ""
echo "=== Baseline Validation ==="
for baseline in evals/baselines/smoke/*.json; do
    name=$(basename "$baseline" .json)
    python3 -c "
import json
with open('$baseline') as f:
    data = json.load(f)
assert 'scenario' in data, 'Missing scenario'
assert 'tool_sequence' in data, 'Missing tool_sequence'
print(f'    OK: {data[\"scenario\"]}')
" || {
        echo "  FAIL: $baseline"
        exit 1
    }
done

# Test the tool decorator in isolation
echo ""
echo "=== Tool Decorator Test ==="
cd "$ROOT_DIR/sdk/python"
python3 -m pytest tests/test_tool_decorator.py -v --tb=short 2>&1 | tail -20

echo ""
echo "=== Integration Test Complete ==="
echo "All validations passed."
