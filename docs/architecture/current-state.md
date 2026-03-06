# Gauntlet Architecture: Current State (Stage 0 Inventory)

Generated as part of the adversarial review remediation, Stage 0.

---

## Package Map

### `cmd/gauntlet/` — CLI entry point
- **main.go** (1,794 lines): All 13 CLI commands in a single file.
  Commands: `run`, `enable`, `record`, `replay`, `discover`, `baseline`, `diff`,
  `report`, `serve`, `version`, `policy`, `review`, `check`.
- Owns: CLI argument parsing, command dispatch, `gauntlet.yml` loading.

### `internal/runner/` — Scenario orchestration
- Loads scenarios, assembles world state, invokes TUT, evaluates assertions, writes output.
- Imports: `assertions`, `baseline`, `determinism`, `docket`, `output`, `scenario`, `tut`, `world`.
- Contains egress probing logic (`egress.go`). Sandbox wrapping unified in `internal/sandbox/` (Stage 1).

### `internal/tut/` — Test-Under-Test execution
- Two harnesses: `cli.go` (subprocess) and `http.go` (HTTP server).
- `process.go`: env shaping (`mergedProcessEnv`), sandbox wrapping (Linux `unshare`, macOS `sandbox-exec`).
- Platform branching: `buildSandboxedCommand()` selects isolation strategy at runtime.

### `internal/proxy/` — MITM HTTP proxy
- Core interception mechanism (localhost:7431).
- `providers/`: Per-provider request normalizers (Anthropic, OpenAI, generic).
- Serves fixture responses in recorded mode.

### `internal/fixture/` — Content-addressed fixture store
- SHA-256 keyed. Canonicalization uses a denylist (unknown fields preserved).
- `store.go`: Thread-safe fixture index loading.

### `internal/assertions/` — Assertion engine
- 7 assertion types registered in `registry.go`.
- Each implements `Assertion` interface: `Evaluate(ctx, result) → Pass/Fail`.
- Hard gates vs soft signals classified in `internal/docket/`.

### `internal/determinism/` — Determinism enforcement
- Verifies fixture hash matches, timestamp freezing, RNG seeding.

### `internal/redaction/` — Secret redaction
- Redacts before disk write (invariant #5).

### `internal/world/` — World state assembly
- Loads tool definitions, DB seeds, and variant policies.
- `variant_policy.go`: References `docs/variant-policy.md`.

### `internal/discovery/` — Auto-discovery pipeline
- Scans for `@gauntlet.tool` decorators, tool world defs, DB schemas.
- Python decorator discovery duplicated in `internal/scaffold/scaffold.go`.

### `internal/scaffold/` — Scaffolding / AST extraction
- `scaffold.go:219`: Shells out to `python3 -c` for AST extraction.
- Duplicates Python tool discovery from `internal/discovery/`.

### `internal/api/` — Local API server
- REST endpoints for UI (proposals, pairs, health, results, baselines, runs, scenarios).
- Mutation endpoints: `/api/proposals/approve`, `/api/proposals/reject`.
- **No authentication.** Wildcard CORS. Localhost-only by convention.

### `internal/ci/` — CI integration
- `enable.go`: Generates `gauntlet.yml` policy template.
- GitHub Actions workflow generation.

### `internal/baseline/` — Baseline comparison
### `internal/docket/` — Failure classification
### `internal/iopairs/` — IO pair libraries
### `internal/output/` — Artifact writing
### `internal/policy/` — Policy loading and validation
### `internal/scenario/` — Scenario YAML parsing
### `internal/ctxversion/` — Context versioning
### `internal/install/` — Install script management

### `sdk/python/gauntlet/` — Python SDK
- `decorators.py`: `@gauntlet.tool` decorator (fixture-backed interception).
- `events.py`: Event emission for traces.
- `connect.py`: SDK connection and environment setup.
- `adapters/`: Optional enrichment adapters (Anthropic, OpenAI, LangChain).

---

## Process-Launch Paths

All subprocess execution sites in the codebase:

| Location | What it launches | Sandbox? |
|----------|-----------------|----------|
| `internal/tut/cli.go:201` | TUT as CLI subprocess | Yes (via `process.go` wrappers) |
| `internal/tut/http.go:42` | TUT as HTTP server | Yes (via `process.go` wrappers) |
| `internal/scaffold/scaffold.go:219` | `python3 -c` for AST extraction | **No** |
| `internal/runner/runner.go:949` | `git rev-parse --short HEAD` | **No** |
| `cmd/gauntlet/baseline_policy.go:137` | `git diff` for baseline comparison | **No** |
| `internal/runner/egress.go:124,137` | `sandbox-exec` / `unshare` wrappers | N/A (is the sandbox) |
| `internal/tut/process.go:107-191` | Platform-specific sandbox wrappers | N/A (is the sandbox) |

### Sandbox Wrappers (in `internal/tut/process.go`)

Six wrapper functions provide platform-specific process isolation:
- `mergedProcessEnv()` — Env shaping (allowlist vs inherit-all)
- `buildSandboxedCommand()` — Dispatch: Linux → `unshare --net`, macOS → `sandbox-exec`
- `wrapWithUnshare()` — Linux network namespace isolation
- `wrapWithSandboxExec()` — macOS sandbox profile
- `wrapWithEgressBlock()` — Fallback egress blocking

**Duplication:** `internal/runner/egress.go` reimplements `sandbox-exec` and `unshare` wrapping independently of `tut/process.go`.

### Environment Shaping

`mergedProcessEnv(overrides, restrictHostEnv)`:
- `restrictHostEnv=true`: Allowlist of 11 vars (PATH, HOME, TMPDIR, TMP, TEMP, SHELL, LANG, LC_ALL, LC_CTYPE, TERM, USER).
- `restrictHostEnv=false`: Inherits entire host environment, then applies overrides.

---

## Mutation Endpoints

Endpoints that modify state (proposals, artifacts, baselines, review state):

| Endpoint | Method | Auth | Effect |
|----------|--------|------|--------|
| `/api/proposals/approve` | POST | **None** | Approves a proposal, writes to disk |
| `/api/proposals/reject` | POST | **None** | Rejects a proposal, writes to disk |

- **CORS:** Wildcard (`*`), no origin restriction.
- **Binding:** Localhost-only by convention (not enforced in code).
- `saveProposals()` at `api.go:380-384`: Writes proposals to `evals/proposals.yaml`.

---

## Ignored Errors (Critical)

Sites where errors are silently discarded or only logged:

| Location | Error | Severity | Impact |
|----------|-------|----------|--------|
| ~~`runner/runner.go:229`~~ | ~~`_ = output.WriteArtifactBundleWithLimit(...)`~~ | ~~**Critical**~~ | **Fixed (Stage 5)** — errors now collected and returned |
| ~~`api/api.go:382-384`~~ | ~~`log.Printf("WARN: ...")` on `saveProposals` failure~~ | ~~**Critical**~~ | **Fixed (Stage 3)** — errors now returned as HTTP 500 |
| `runner/runner.go` | `getCommit()` returns `"unknown"` on error | Low | Cosmetic — commit hash in run metadata |
| `tut/process.go` | Multiple `exec.Command` wrapper sites | Medium | Sandbox setup failures may not propagate |
| `fixture/store.go` | Index load errors in some paths | Medium | Could mask fixture corruption |

---

## Known Duplications

1. **Python discovery**: Both `internal/discovery/python.go` and `internal/scaffold/scaffold.go` scan for `@gauntlet.tool` / `@tool` decorators via separate mechanisms. Discovery uses regex; scaffold shells out to `python3 -c` for AST parsing.

2. ~~**Egress wrapping**: Both `internal/runner/egress.go` and `internal/tut/process.go` independently implement `sandbox-exec` (macOS) and `unshare --net` (Linux) command wrapping with different argument construction.~~ **Fixed (Stage 1)** — unified in `internal/sandbox/sandbox.go`.

3. **Adapter boilerplate**: `sdk/python/gauntlet/adapters/openai.py` and `anthropic.py` are ~95% identical (same `_to_plain`, `_extract_payload`, `_extract_endpoint`, `_emit_model_call`, `_wrap_callable`, `_instrument_method`, `_instrument_resource_create`, `_instrument_client`, `_patch_client_constructor`). Only the provider name and resource paths differ.

---

## Trust Boundary Owners

| Package | Trust boundary | Key invariant |
|---------|---------------|---------------|
| `proxy` | Network egress interception | PR CI never makes real network egress |
| `runner` | Scenario execution isolation | Budget enforcement stops after current scenario |
| `determinism` | Fixture integrity | Hash misses are hard failures, never live fallbacks |
| `redaction` | Secret protection | Redaction before disk write, never after |
| `ci` | Fork PR safety | Fork PRs never receive secrets |

---

## Key Architectural Risks (from adversarial review)

1. **1,794-line main.go**: All CLI logic in one file — high merge conflict risk, hard to test commands in isolation.
2. **No API auth**: Mutation endpoints accessible to any local process.
3. **Duplicated sandbox logic**: Divergence risk between `runner/egress.go` and `tut/process.go`.
4. **Silent artifact write failures**: `runner.go:229` discards errors, making debugging harder.
5. **Unsandboxed subprocess calls**: `scaffold.go:219` (python3) and `runner.go:949` (git) bypass sandbox.
6. **Full env inheritance**: When `restrictHostEnv=false`, secrets in host env leak to TUT.
