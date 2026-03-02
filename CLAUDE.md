# CLAUDE.md — Gauntlet

## What this is

Gauntlet is a deterministic CI-native scenario testing tool for agentic systems.
Core principle: freeze the world (tools, model calls, DBs), vary the scenarios.

## The most important architectural fact

**Model call interception works via HTTP proxy, not SDK wrapping.**

Gauntlet runs a local MITM proxy at localhost:7431. All model calls from the
TUT are routed through it via HTTPS_PROXY env var injection. The proxy serves
fixture responses in recorded mode. The TUT requires zero code changes.

SDK adapters are OPTIONAL enrichment for richer traces. Never required.

## Tool call interception

Tool calls are intercepted via the `@gauntlet.tool` Python decorator.
In recorded mode, the decorator returns fixture responses WITHOUT calling
the underlying function. The underlying function NEVER executes in PR CI.
This is an interceptor, not an observer.

## Key invariants — never break these

1. PR CI mode NEVER makes real network egress (enforced at OS level)
2. Fork PRs NEVER receive secrets
3. Hard gates are ALWAYS deterministic — judge scores NEVER block in v1
4. Budget enforcement stops after current scenario, never mid-execution
5. Redaction happens before disk write, never after
6. Fixture hash misses are hard failures, never live fallbacks
7. The underlying tool function NEVER executes when a fixture is available
8. Canonicalization uses a DENYLIST — unknown fields are preserved
9. `output.sensitive_leak` is ALWAYS a soft signal, never a hard gate

## Build and test

```bash
make build          # build gauntlet binary
make test           # run all Go tests with -race
make test-example   # integration test against examples/support-agent
make lint           # golangci-lint
make proxy-test     # run proxy interception tests specifically
```

## Directory rules

- All Go implementation: `internal/`
- All CLI commands: `cmd/gauntlet/`
- Python SDK only: `sdk/python/gauntlet/`
- JSON Schemas: `schema/`
- Example agents: `examples/`
- Scenarios: `evals/smoke/` or `evals/full/`
- Tool world definitions: `evals/world/tools/`
- DB world definitions: `evals/world/databases/`
- Fixtures: `evals/fixtures/tools/` and `evals/fixtures/models/`
- Baselines: `evals/baselines/<suite>/`
- Run artifacts: `evals/runs/<timestamp>-<commit>/`

## Error message standard

Every user-facing error must include:
- What failed
- Expected vs actual (where applicable)
- File path and line number (where applicable)
- Docket tag (for assertion failures)
- Exact command to fix it

NEVER output bare Go error strings to the user.

## Adding a new provider normalizer

1. Create `internal/proxy/providers/<name>.go`
2. Implement `ProviderNormalizer` interface
3. Add detection rule to `internal/proxy/providers/detector.go`
4. Add test in `internal/proxy/providers/<name>_test.go`
5. Add to `PROVIDER_SUPPORT.md` compatibility matrix
6. Add a fixture canonicalization test proving identical hash
   across equivalent requests from different SDK versions

## Adding a new assertion type

1. Create `internal/assertions/<name>.go` implementing `Assertion` interface
2. Register in `internal/assertions/registry.go`
3. Add JSON Schema entry in `schema/scenario.schema.json`
4. Add test with at least: passing case, failing case, edge case
5. Add docket classification rule in `internal/docket/classifier.go`
6. Document whether it is a hard gate or soft signal

## Do not build in v1

- Always-on local daemon
- VS Code extension (expose :7432 HTTP API only — community builds extension)
- Golden baselines (skeleton only — log warning when used)
- Judge scoring as a blocking gate
- Multi-fault chaos scenarios (accept the flag, log a warning, run anyway)
- Multi-backend storage beyond filesystem
- Deep repo parsing with automated manifest PRs
- Streaming response recording (log unsupported warning)
