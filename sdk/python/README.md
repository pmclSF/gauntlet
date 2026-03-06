# gauntlet-sdk

Python SDK for [Gauntlet](https://github.com/pmclSF/gauntlet) — deterministic scenario testing for agentic systems.

## Install

```bash
pip install gauntlet-sdk
```

With framework adapters:
```bash
pip install gauntlet-sdk[openai]      # OpenAI SDK support
pip install gauntlet-sdk[anthropic]   # Anthropic SDK support
pip install gauntlet-sdk[langchain]   # LangChain support
```

## Usage

```python
import gauntlet_sdk as gauntlet

# Connect to the Gauntlet runner (no-op if not running; safe in production)
gauntlet.connect()

# Decorate tools for fixture interception
@gauntlet.tool(name="my_tool")
def my_tool(arg: str) -> dict:
    return call_external_api(arg)
```

In recorded mode (PR CI), `@gauntlet.tool` returns fixture responses without calling the underlying function. In production, it runs normally.

Note: the canonical import namespace is `gauntlet_sdk` to avoid collisions with the unrelated `gauntlet` package on PyPI.

## License

MIT
