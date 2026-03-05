# Quickstart

Get Gauntlet running against your agent in 5 minutes.

## 1. Install

```bash
go install github.com/pmclSF/gauntlet/cmd/gauntlet@latest
```

## 2. Initialize your project

```bash
cd your-agent-repo
gauntlet init
```

This generates:
- `.github/workflows/gauntlet.yml` — CI workflow
- `evals/gauntlet.yml` — policy file
- `evals/smoke/` — starter scenario directory

## 3. Add the SDK hook

```python
# At the top of your agent entrypoint
import gauntlet
gauntlet.connect()
```

Install the SDK:
```bash
pip install gauntlet-sdk  # or: pip install -e path/to/gauntlet/sdk/python
```

## 4. Wrap your tools

```python
@gauntlet.tool(name="my_tool")
def my_tool(arg: str) -> dict:
    # In PR CI: fixture response returned, this code never runs
    return call_external_api(arg)
```

## 5. Discover tools and scenarios

```bash
gauntlet discover
```

This scans your codebase for `@gauntlet.tool`, `@function_tool`, `@agent.tool`, and `@tool` decorators, then generates scenario YAML files automatically.

## 6. Run

```bash
gauntlet run --suite smoke
```

## 7. Review results

```bash
gauntlet review
```

Opens a browser UI at `http://localhost:7432` showing test results, proposals, and baselines.

## Next steps

- Record fixtures from live runs: `gauntlet record --suite smoke`
- Establish baselines: `gauntlet baseline --suite smoke`
- Check project health: `gauntlet doctor --suite smoke`
- Push and let CI gate your PR

See the [README](../README.md) for full documentation on scenarios, world definitions, assertions, and CI behavior.
