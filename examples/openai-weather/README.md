# OpenAI Weather Example

A synthetic weather agent demonstrating simple tool routing and forbidden-tool assertions.

## What it demonstrates

- Single `get_weather` tool with `@gauntlet.tool` decoration
- City extraction heuristics from user queries
- Intentional bug: calls `get_weather` even for non-weather requests
- Forbidden-tool assertions catching incorrect tool invocations

## Prerequisites

- Gauntlet CLI installed
- Python 3.10+

## Run

```bash
cd examples/openai-weather
pip install -e ../../sdk/python
gauntlet run --suite smoke
```
