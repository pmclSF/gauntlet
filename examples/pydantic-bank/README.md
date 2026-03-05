# Pydantic Bank Example

A synthetic bank support agent demonstrating structured output testing and database-backed tools.

## What it demonstrates

- Structured dict output with `support_advice`, `block_card`, `risk`, `tool_calls`
- SQLite database tool (`customer_balance`) with Gauntlet fixture interception
- Intentional bugs: permission violations (querying other customers) and data leaks (SSN exposure)
- Assertions that catch these security violations

## Prerequisites

- Gauntlet CLI installed
- Python 3.10+

## Run

```bash
cd examples/pydantic-bank
pip install -e ../../sdk/python
gauntlet run --suite smoke
```
