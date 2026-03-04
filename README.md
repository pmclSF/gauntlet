# Gauntlet

**Deterministic scenario testing and CI quality gates for agentic systems.**

Every PR runs the gauntlet before it ships.

---

## What it is

Gauntlet freezes the world — tools, model calls, databases — and tests your
agent's behavior against that frozen world. When the agent code, prompt, or
planner changes, Gauntlet detects regressions before they ship.

This is not an eval dashboard. It is a test runner with a gate.

---

## Quickstart (10 minutes)

### 1. Install

```bash
# macOS/Linux
curl -fsSL https://gauntlet.dev/install.sh | sh

# Or build from source
git clone https://github.com/gauntlet-dev/gauntlet.git
cd gauntlet && make build
export PATH="$PWD/bin:$PATH"

# Verify
gauntlet --version
```

### 2. Add the hook to your agent

```python
# One line, at the top of your agent entrypoint
import gauntlet
gauntlet.connect()  # no-op if Gauntlet not running; safe in production
# connect() also enables OpenAI/Anthropic transport hooks and LangChain callback hooks when available.
```

Optional explicit adapter helpers (advanced):

```python
from gauntlet.adapters import patch_openai_client, patch_anthropic_client, patch_langchain_llm

client = patch_openai_client(client)
anthropic_client = patch_anthropic_client(anthropic_client)
llm = patch_langchain_llm(llm)
```

### 3. Wrap your tools

```python
@gauntlet.tool(name="order_lookup")
def lookup_order(order_id: str) -> dict:
    # In PR CI: fixture response returned, this code never runs
    # In production: runs normally
    return requests.get(f"https://api.example.com/orders/{order_id}").json()
```

### 4. Enable CI

```bash
cd your-agent-repo
gauntlet enable
```

This generates:
- `.github/workflows/gauntlet.yml` — CI workflow
- `evals/gauntlet.yml` — policy file
- `evals/smoke/` — starter scenario directory

### 5. Create your first scenario

```yaml
# evals/smoke/order_status.yaml
scenario: order_status_nominal
description: "User asks for order status — happy path"

input:
  messages:
    - role: user
      content: "What's the status of my order ord-001?"

world:
  tools:
    order_lookup: nominal
  databases:
    orders_db:
      seed_sets: [standard_order]

assertions:
  - type: output_schema
    schema: {type: object, required: [response], properties: {response: {type: string}}}
  - type: tool_sequence
    required: [order_lookup]
  - type: tool_args_invariant
    tool: order_lookup
    invariant: "args.order_id is not null"
```

### 6. Record fixtures and establish baseline

```bash
# Record tool and model fixtures from a trusted run
GAUNTLET_MODEL_MODE=live gauntlet record --suite smoke
# This also signs fixtures and generates .gauntlet/fixture-signing-key.pem(.pub.pem)
# Commit only `.gauntlet/fixture-signing-key.pem.pub.pem` (never commit private key).

# Establish contract baseline
gauntlet baseline --suite smoke

# Baseline updates also emit rollback artifacts:
#   evals/baselines/<suite>/rollback.manifest.json
#   evals/baselines/<suite>/ROLLBACK_PR_TEMPLATE.md

# When canonical hash version changes, migrate existing fixtures
gauntlet migrate-fixtures --from-version 1 --to-version 2 --dry-run
gauntlet migrate-fixtures --from-version 1 --to-version 2

# Generate/update replay lockfile for deterministic tamper detection
gauntlet lock-fixtures --suite smoke

# Sign run artifacts with an evidence manifest (also done by generated CI workflow)
gauntlet sign-artifacts --dir evals/runs
```

### 7. Run the suite

```bash
gauntlet run --suite smoke
# Optional: strict policy parsing (unknown keys are hard errors)
gauntlet run --policy-strict --suite smoke
# Explicit runner/model mode separation:
gauntlet run --suite smoke --runner-mode pr_ci --model-mode recorded
# Add deterministic per-scenario timeout budget (ms):
gauntlet run --suite smoke --budget 300000 --scenario-budget 45000

# Preflight checks for policy, modes, proxy trust, fixtures, and egress:
gauntlet doctor --suite smoke
```

### 8. Push and watch CI gate your PR

---

## Integration levels

Choose the level that fits your setup:

| Level | What you need | What you get |
|---|---|---|
| **Best** | HTTP endpoint + `gauntlet.connect()` | Full scenario testing, tool traces, model replay |
| **Good** | CLI entrypoint + `gauntlet.connect()` | Full scenario testing, tool traces, model replay |
| **Minimal** | Just a CLI entrypoint | Egress enforcement + exit code gate + budget enforcement |

Minimal still provides real value. Even without structured traces, blocking network
egress and enforcing a time budget catches a class of regressions most CI setups miss.

## SDK capability negotiation

Gauntlet SDKs emit a versioned `sdk_capabilities` handshake (`protocol_version: 1`)
into the trace stream. The runner consumes this negotiation report and emits a soft
`adapter_capabilities` diagnostic when:

- capability negotiation is missing
- protocol version is unsupported
- an adapter is enabled but runtime patching failed

This keeps CI deterministic while still surfacing integration drift early.

---

## Scenario format

```yaml
scenario: <unique_name>
description: "<human readable description>"

input:
  messages:                        # OpenAI-format messages
    - role: user
      content: "..."
  # OR
  payload:                         # Arbitrary JSON payload
    key: value

world:
  tools:
    <tool_name>: <state_name>      # e.g. order_lookup: nominal
  databases:
    <db_name>:
      seed_sets: [<seed_name>]

assertions:
  - type: output_schema
    schema: <JSON Schema object>
  - type: tool_sequence
    required: [tool_a, tool_b]     # must appear in this order
    forbidden: [tool_c]            # must NOT appear
  - type: retry_cap
    tool: order_lookup
    max_retries: 2
  - type: tool_args_invariant
    tool: order_lookup
    invariant: "args.order_id == input.order_id"
```

---

## World definitions

### Tool state envelope

```yaml
# evals/world/tools/order_lookup.yaml
tool: order_lookup

states:
  nominal:
    response:
      order_id: "ord-001"
      status: "confirmed"
      total_cents: 4999
    agent_expectation: "completes normally"

  timeout:
    behavior: delay_ms 8000
    agent_expectation: "retries once, then surfaces error"

  server_error:
    response_code: 500
    agent_expectation: "retries with backoff, caps at 2"
```

### DB seed definition

```yaml
# evals/world/databases/orders_db.yaml
database: orders_db
adapter: sqlite

seed_sets:
  standard_order:
    table: orders
    rows:
      - id: "ord-001"
        status: "confirmed"
        total_cents: 4999
```

Single-fault policy counts both non-nominal tool states and non-nominal DB seed variants.
Set `chaos: true` in a scenario to allow multi-fault combinations intentionally.

## Mode semantics

- `runner_mode` / `--runner-mode`: execution context (`local`, `pr_ci`, `fork_pr`, `nightly`)
- `model_mode` / `--model-mode`: fixture behavior (`recorded`, `live`, `passthrough`)
- `mode` / `--mode`: legacy compatibility alias; prefer explicit runner/model fields and flags
- `proxy.mode`: model replay mode only (`recorded`, `live`, `passthrough`)

---

## CI behavior

### PR CI (hermetic)
- Zero network egress — enforced at process level
- Mandatory outbound socket egress self-test before scenario execution
- Recorded replay mode verifies `evals/fixtures/replay.lock.json` integrity before execution
- Replay lockfile/canonical hash determinism is snapshot-tested against a cross-platform runtime matrix (`linux/darwin/windows`, `amd64/arm64`)
- Recorded replay mode enforces fixture signatures against `.gauntlet/fixture-signing-key.pem.pub.pem`
- Generated CI workflow runs `gauntlet scan-artifacts` and `gauntlet sign-artifacts` before upload-artifact
- `scan-artifacts` includes a default prompt-injection marker denylist for recorded artifacts (opt-out: `redaction.prompt_injection_denylist: false`)
- Optional hard allowlist for recorder identities via `GAUNTLET_TRUSTED_RECORDER_IDENTITIES`
- Tool and model calls served from fixtures
- Hard/soft assertion gating enforced from `evals/gauntlet.yml` (`assertions.hard_gates`, `assertions.soft_signals`)
- Baseline-changing PRs must carry label `gauntlet/baseline-approved` (enforced by `gauntlet check-baseline-approval`)
- Fork PRs: replay-only, no secrets, no judge calls
- Target: < 5 minutes
- Proxy enforces deterministic request parsing limits (header/body/request-count per connection)
- Malformed JSON and unsupported websocket/HTTP2 tunnel traffic return explicit proxy error codes
- Proxy CA assets require hardened permissions; doctor reports rotation warning before cert expiry
- CLI failures emit canonical machine-parseable error code lines (`GAUNTLET_ERROR_CODE=<code>`)
- Fixture misses include provider/model/hash context plus nearest recorded fixture candidates
- Results include deterministic per-scenario causal taxonomy (`failure_category`) and effective scenario budgets
- Results include lightweight run-history metadata (`history.previous`, `history.delta`) for regression velocity tracking
- Per-scenario runtime determinism checks verify timezone/locale freeze application when SDK reports `determinism_env`
- Non-Python SDKs emit explicit nondeterminism guard warnings until runtime freeze verification parity is available
- Optional per-scenario TUT process limits available via `tut.resource_limits` (`cpu_seconds`, `memory_mb`, `open_files`)
- Optional linux hostile-payload hardening via `tut.guardrails.hostile_payload` (+ `max_processes`)

### Nightly (trusted)
- Live model calls (secrets available)
- Fixture re-recording
- Proposes baseline update PR if behavior changed
- Full suite (no time constraint)

---

## Failure output

Every failure produces a self-contained artifact:

```
FAILED  order_status_conflicting_payment

Culprit: db.seed.conflicting_state
Confidence: high

Failing assertion:
  tool_sequence
  Expected: [order_lookup, payment_lookup]
  Actual:   [order_lookup]              <- payment_lookup never called

World state:
  tools:     order_lookup -> nominal
  databases: orders_db -> conflicting_state
               orders.ord-007.status   = "confirmed"
               payments.ord-007.status = "failed"  <- conflict not surfaced

Baseline output: "Your order shows confirmed but the payment failed..."
PR output:       "Your order ord-007 is confirmed."

Docket tag: planner.premature_finalize
```

---

## FAQ

**Is this an eval platform?**
No. Gauntlet does not have a dashboard, scoring UI, or human labeling workflow.
It is a test runner that gates CI. Think pytest for agent behavior.

**Why not LangSmith / vendor X?**
Those are observability and eval platforms — valuable for different things.
Gauntlet is CI-native, offline-capable, and enforces contracts.
It does not require a cloud account to run.

**Does it work with my framework?**
If your agent can be invoked via HTTP or CLI, it works with Gauntlet.
`gauntlet.connect()` has adapters for OpenAI SDK, Anthropic SDK, and LangChain.
Other frameworks work via the CLI adapter or HTTP endpoint.

**What about multi-agent systems?**
Multi-agent is on the roadmap. v1 tests single-agent behavior.
For multi-agent: define each agent as a separate TUT with its own suite.

---

## Contributing

The most useful contributions right now:
- New tool state patterns (open an issue with the pattern)
- Framework adapters in `sdk/python/gauntlet/adapters/`
- Additional assertion types in `internal/assertions/`
