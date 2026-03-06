# Code Agent Example

This example demonstrates a code-generation flow with sandbox execution and
content safety assertions.

What it demonstrates:
- Tool-backed code execution via `sandbox_exec`
- `forbidden_content` check to prevent credential-like leakage in output
- `tool_sequence` + `output_schema` for deterministic contract checks

Run smoke suite:

```bash
cd examples/code-agent
../../bin/gauntlet run --suite smoke --auto-discover=false
```
