# Orchestrator Example

This example demonstrates a multi-tool orchestrator that chains planning,
context fetch, and response finalization.

What it demonstrates:
- 3+ tool chain with strict `tool_sequence`
- Retry behavior with `retry_cap`
- Recovery from a transient mid-chain failure

Run smoke suite:

```bash
cd examples/orchestrator
../../bin/gauntlet run --suite smoke --auto-discover=false
```
