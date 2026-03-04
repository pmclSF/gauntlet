# Gauntlet — Onboarding Guide

## Quick Start

### 1. Install Gauntlet

```bash
go install github.com/gauntlet-dev/gauntlet/cmd/gauntlet@latest
```

### 2. Enable Gauntlet in your project

```bash
cd your-agent-project
gauntlet enable
```

This generates:
- `.github/workflows/gauntlet.yml` — CI workflow
- `evals/gauntlet.yml` — Policy file
- Prints framework-specific setup instructions

### 3. Integrate the SDK

#### Python SDK

```bash
pip install gauntlet-sdk
```

Add to your agent entrypoint:

```python
import gauntlet
gauntlet.connect()  # no-op if Gauntlet not running; safe in production
```

#### Decorate tools with `@gauntlet.tool`

```python
import gauntlet

@gauntlet.tool(name="order_lookup")
def lookup_order(order_id: str) -> dict:
    # This function is NOT called in PR CI — fixture returned instead
    return requests.get(f"https://api.example.com/orders/{order_id}").json()
```

When `GAUNTLET_ENABLED=1` and `GAUNTLET_MODEL_MODE=recorded`:
- The underlying function is **never called**
- Fixture response is returned directly from the content-addressed store
- Tool call is recorded in the trace

When not in Gauntlet mode, the real function runs transparently.

#### Handle database env vars

```python
import os
DATABASE_URL = os.environ.get("GAUNTLET_DB_ORDERS", "sqlite:///./orders.db")
```

Gauntlet injects `GAUNTLET_DB_<NAME>` env vars pointing to ephemeral SQLite instances seeded from your world definition.

### 4. Write your first scenario

Create `evals/smoke/hello_world.yaml`:

```yaml
scenario: hello_world
description: "Basic smoke test"

input:
  messages:
    - role: user
      content: "Hello, what can you help me with?"

world:
  tools:
    my_tool: nominal

assertions:
  - type: output_schema
    schema:
      type: object
      required: ["response"]
  - type: sensitive_leak
    patterns: ["credit_card", "ssn"]
```

### 5. Define tool world states

Create `evals/world/tools/my_tool.yaml`:

```yaml
tool: my_tool
description: My tool
states:
  nominal:
    response:
      result: "success"
  timeout:
    delay_ms: 30000
    error: "connection timed out"
  server_error:
    status_code: 500
    error: "Internal Server Error"
  malformed_response:
    response: "not valid json"
```

### 6. Record fixtures

```bash
GAUNTLET_MODEL_MODE=live gauntlet record --suite smoke
```

This runs your agent with real API calls and records responses as fixtures.

### 7. Run the suite

```bash
gauntlet run --suite smoke
```

In CI, this runs automatically on every PR.

## Quick start with auto-discovery

1. Define your tools in `evals/world/tools/` (or use `gauntlet scaffold`)
2. Run: `gauntlet run --suite smoke --dry-run`
3. Auto-discovery generates scenarios automatically
4. Review generated scenarios in `evals/smoke/auto_*.yaml`
5. Optionally add IO pairs in `evals/pairs/` for richer test inputs
6. Record fixtures: `gauntlet record --suite smoke`
7. Run for real: `gauntlet run --suite smoke`

## Integration Levels

| Level | What it needs | What Gauntlet checks |
|-------|--------------|---------------------|
| **Best** | HTTP + `@gauntlet.tool` | Everything: tool traces, model traces, all assertions |
| **Good** | HTTP only (no decorator) | Model traces via proxy, output assertions, schema |
| **Minimal** | CLI only (no changes) | Exit code, budget enforcement, egress blocking |

## Determinism Layers

Gauntlet freezes 7 sources of nondeterminism:

1. **Network** — MITM proxy intercepts all HTTP/HTTPS calls
2. **Tools** — `@gauntlet.tool` short-circuits to fixtures
3. **Database** — Ephemeral SQLite seeded from world definitions
4. **Time** — `GAUNTLET_FREEZE_TIME` patches datetime/time
5. **RNG** — `GAUNTLET_RNG_SEED` seeds random/numpy
6. **Locale** — `GAUNTLET_LOCALE` + `GAUNTLET_TIMEZONE` fixed
7. **Output** — JSON canonicalized before comparison

## Proxy Architecture

All model calls from your agent are intercepted by the Gauntlet MITM proxy:

```
Agent → HTTPS_PROXY → Gauntlet Proxy → Detect Provider → Normalize → Hash → Fixture Lookup
```

Supported providers: OpenAI, Anthropic, Google AI, AWS Bedrock, Cohere, Ollama, and any OpenAI-compatible API.

No SDK changes needed — the proxy intercepts at the HTTP level.

## Assertion Types

### Hard gates (block PR merge)
- `output_schema` — JSON Schema validation
- `tool_sequence` — Required tool call order
- `tool_args` — Tool argument validation
- `retry_cap` — Max consecutive retries (default 3)
- `forbidden_tool` — Tools that must not be called

### Soft signals (warnings only)
- `output_derivable` — Output grounded in fixture/DB data
- `sensitive_leak` — PII detection (credit cards, SSNs, API keys)

## Review UI

Launch the review UI to manage proposals and inspect results:

```bash
gauntlet review --evals evals --static ui/dist
```

Then open `http://localhost:7432` in your browser.

## Discovery Engine

Auto-discover test proposals from your codebase:

```bash
gauntlet discover \
  --tools evals/world/tools \
  --python-dirs agent \
  --db-schemas evals/world/databases \
  --exclude-tools send_email \
  --output evals/proposals.yaml
```

Review and approve proposals in the UI or YAML file.

`gauntlet run` now performs this discovery step automatically by default and
materializes `auto_*.yaml` scenarios when the target suite is empty.
