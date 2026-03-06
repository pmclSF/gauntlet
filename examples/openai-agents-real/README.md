# OpenAI Agents (Real Framework) Example

Proves Gauntlet integrates with the real OpenAI Agents SDK (`agents.Agent`, `Runner`, `function_tool`).

## What it demonstrates

- Real `agents` package imports for agent and tool declarations
- Real `pydantic.BaseModel` for structured weather output
- `@gauntlet.tool` wrappers around `@function_tool`-style signatures
- Phase 1: rule-based handler with real framework types
- Path to Phase 2: replace rules with actual `Runner.run()` calls intercepted by MITM proxy

## Prerequisites

- Gauntlet CLI installed
- Python 3.10+
- `pip install openai-agents`

## Run

```bash
cd examples/openai-agents-real
pip install -e ../../sdk/python
pip install openai-agents
gauntlet run --suite smoke
```
