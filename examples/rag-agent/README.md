# RAG Agent Example

This example demonstrates a retrieval-augmented support flow with deterministic
scenario tests.

What it demonstrates:
- Retrieval world states: `nominal`, `empty_results`, `timeout`
- `tool_sequence` checks for retrieval call ordering
- `output_schema` checks that responses include `citations`

Run smoke suite:

```bash
cd examples/rag-agent
../../bin/gauntlet run --suite smoke --auto-discover=false
```
