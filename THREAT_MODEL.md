# Gauntlet — Threat Model

## Trust boundaries

### Fork PR (untrusted)
- Source: external contributor
- Secrets: NONE — GitHub does not expose secrets to fork PRs
- Mode: replay-only, contract checks only
- Network: blocked at container level
- Can it update baselines? NO — baseline writes require trusted context
- Can it exfiltrate credentials? NO — no secrets present, network blocked
- Can it DoS the runner? Mitigated by CPU/mem/disk limits, scenario timeout, and optional linux hostile-payload guardrails (`tut.guardrails.hostile_payload`)

### Same-repo PR (semi-trusted)
- Source: internal contributor or bot
- Secrets: available if configured
- Mode: replay + optional judge calls
- Network: blocked for agent execution, allowed for judge calls if configured
- Can it update baselines? NO — only nightly workflow can propose baseline PRs

### Nightly (trusted)
- Source: scheduled workflow on main/release
- Secrets: full access
- Mode: live model calls, fixture re-recording
- Network: allowed for agent execution and model calls
- Can it update baselines? YES — proposes a PR, requires human approval

## What data Gauntlet captures

### In CI (all modes)
- Scenario input payloads (as defined in YAML — no live user data)
- Tool fixture responses (pre-recorded, no live data)
- Agent outputs (for comparison against baseline)
- Tool call traces (arg/return shapes from fixture layer)
- Assertion results

### In local development (daemon, v2)
- Structured events from gauntlet.connect() hook
- Process-level: command, exit code, wall clock time
- Explicitly NOT captured by default: raw stdout/stderr, environment variables
- Env capture: allowlist only, never default-on

## Redaction guarantees

All data is redacted before disk write. Redaction is not a post-processing step.

Fields redacted by default:
- **.api_key, **.password, **.token, **.secret, **.authorization
- **.x-api-key (HTTP headers)

Patterns redacted by default:
- Redaction write-path (`RedactJSON` / `RedactString`):
  - Credit-card-like digit sequences (regex replacement)
  - Social Security Numbers (XXX-XX-XXXX)

`gauntlet scan-artifacts` detection stack (composable detectors):
- Credit card detector with Luhn validation (13-19 digit candidates)
- Token format detector (e.g., `sk-*`, `ghp_*`, `AKIA*`, `AIza*`, Slack token forms)
- Contextual keyword detector (`api_key`, `token`, `secret`, `password`, etc.)
- Prompt-injection marker denylist detector (default on; policy opt-out supported)
- High-entropy token detector (including printable segments inside binary files)
- SSN regex detector

Configurable in gauntlet.yml:
- Additional field paths (JSONPath syntax)
- Additional regex patterns (legacy extension path)
- Email addresses (off by default)

## Artifact security

Baseline fixtures and failure artifacts are checked into git.
Risk: sensitive data in committed artifacts.

Mitigations:
- `gauntlet scan-artifacts` — runs before commit, detects sensitive content
- Recommended as pre-commit hook
- CI runs scan on every push; fails if sensitive content detected in evals/

## Supply chain

Binary distribution:
- Checksums published in `https://gauntlet.dev/checksums.txt`
- `install.sh` verifies SHA-256 checksums for prebuilt binary installs before extraction
- `go install ...@latest` path relies on the Go module checksum database (sumdb)

GitHub Actions:
- Workflow pinned to SHA, not tag
- No `pull_request_target` usage
- Secrets scoped to trusted workflows only

## What Gauntlet cannot protect against

- A malicious contributor who has write access to the repo (insider threat)
- Scenarios that embed real user data (teams must ensure evals/ contains only synthetic data)
- Fixture files that were recorded with unredacted sensitive data and then committed
  (mitigated by artifact scanner, not eliminated)
