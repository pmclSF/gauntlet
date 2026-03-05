# Pydantic Bank (Real Framework) Example

Proves Gauntlet works with real PydanticAI imports (`pydantic_ai.Agent`, `RunContext`, `BaseModel`).

## What it demonstrates

- Real `pydantic_ai` types for dependency injection and structured output
- `@gauntlet.tool` wrappers around PydanticAI-style tool signatures
- Phase 1: rule-based handler with real framework types
- Path to Phase 2: replace rules with actual `agent.run()` calls intercepted by MITM proxy

## Prerequisites

- Gauntlet CLI installed
- Python 3.10+
- `pip install pydantic-ai`

## Run

```bash
cd examples/pydantic-bank-real
pip install -e ../../sdk/python
pip install pydantic-ai
gauntlet run --suite smoke
```
