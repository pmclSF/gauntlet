# Gauntlet P0 Implementation Plan

## Scope and sequencing
This plan converts all P0 backlog items into dependency-ordered, mergeable batches. Batch 1, Batch 2, Batch 3, Batch 4, and Batch 5 are implemented in this change set.

## P1 follow-on batches

### P1 Batch 1 (implemented in this PR)
P1 items: `26, 27`

- Add policy schema validation on load with field and line diagnostics.
- Add strict policy parsing mode for unknown-key errors (`--policy-strict` / `GAUNTLET_POLICY_STRICT=true`).

### P1 Batch 2 (implemented in this PR)
P1 items: `28, 29`

- Separate runner-mode vs model-mode CLI semantics:
  - add explicit `--runner-mode` (retain `--mode` as legacy alias)
  - add cross-mode validation with actionable guidance on misused flags
  - enforce `proxy.mode` as model-mode-only at policy load
- Add `gauntlet doctor` preflight command to validate:
  - strict policy load
  - resolved runner/model mode semantics
  - egress probe consistency with runner mode
  - proxy bindability + local CA trust assets
  - recorded replay lockfile/trust verification
- Migration note:
  - `proxy.mode` now validates as model-mode-only (`recorded|live|passthrough`).
  - If runner behavior was previously set via `proxy.mode`, move it to `defaults.runner_mode` or `suites.<name>.runner_mode`.

### P1 Batch 3 (implemented in this PR)
P1 items: `21, 22, 23, 42`

- Promote Python SDK provider adapters to real instrumentation:
  - OpenAI and Anthropic adapters now instrument transport/resource call paths and emit deterministic `model_call` events with metadata.
  - LangChain adapter now emits callback-driven tool/model trace events with duration and model context.
- Add explicit no-op warnings when adapters cannot instrument runtime due to missing SDK/callback hooks (`GAUNTLET_ADAPTER_WARNINGS` controls warning output).
- Extend trace event schema ingestion to map provider/model metadata into runner-side model call records.

### P1 Batch 4 (implemented in this PR)
P1 items: `24, 25, 43`

- Add versioned SDK capability negotiation (`sdk_capabilities`, protocol `v1`) from Python SDK to runner ingestion.
- Add runner-side deterministic soft diagnostics (`adapter_capabilities`) when capability negotiation is missing, protocol versions mismatch, or adapters are enabled-but-unpatched.
- Extend transport compatibility coverage for streaming/SSE-adjacent requests:
  - verify stream-flag stripping logic handles `stream_options` safely when converting live requests into non-streaming fixture recordings
  - add edge tests for non-boolean stream values to prevent accidental semantic rewrites
- Add cross-language SDK parity roadmap and compatibility matrix documentation (Python current; JS/TS/Go planned with protocol v1 target).

### P1 Batch 5 (implemented in this PR)
P1 items: `46, 47, 48`

- Harden proxy request handling with deterministic limits:
  - max request header size
  - max request body size
  - max requests per decrypted keep-alive connection
- Add controlled malformed-JSON behavior in intercept path:
  - explicit HTTP 400 + deterministic error code (`malformed_json_request`)
  - remove generic 502 ambiguity for parse failures
- Add explicit protocol edge handling/reporting:
  - websocket upgrade requests return deterministic unsupported response (`websocket_not_supported`)
  - HTTP/2 preface over CONNECT tunnel returns deterministic version error (`http2_not_supported`)
- Add focused transport hardening tests that cover the new limit and protocol-edge paths.

### P1 Batch 6 (implemented in this PR)
P1 items: `45, 49`

- Harden local proxy CA asset storage permissions:
  - require non-symlink CA directory/files
  - require CA directory to not be group/other writable
  - require `ca.key` to be owner-only permissions
  - fail CA load/startup deterministically when permissions are insecure
- Add cert expiration and rotation checks for local CA assets:
  - fail CA load on expired/not-yet-valid certs
  - add proactive doctor warning when CA is within rotation window (`DefaultCARotationWindow`)
- Add tests covering:
  - permission enforcement (`LoadCA` + runtime startup classification)
  - expiry failure behavior
  - rotation warning behavior in `gauntlet doctor`

### P1 Batch 7 (implemented in this PR)
P1 items: `30, 31`

- Replace regex-only artifact leak checks with composable detectors in `scan-artifacts`:
  - token format detector
  - contextual keyword detector
  - high-entropy detector
  - Luhn-backed credit-card detector
  - SSN detector
- Extend scanning beyond extension allowlists using binary-safe printable segment analysis.
- Align threat model documentation to implemented detector behavior:
  - clarify distinction between write-path redaction patterns and scan-time detector stack
  - document Luhn validation specifically in scanner detector path.

### P1 Batch 8 (implemented in this PR)
P1 items: `38, 39`

- Add canonical machine-readable CLI failure codes:
  - emit `GAUNTLET_ERROR_CODE=<code>` on command failures for CI parsing
  - map common failure classes (fixture miss, mode validation, egress self-test, proxy startup root causes, replay integrity, baseline approval, doctor failures)
  - emit deterministic codes for direct process exits in `run` and `scan-artifacts`
- Improve fixture miss diagnostics:
  - include provider family, model, and canonical hash in miss output
  - compute and report nearest recorded fixture candidates (hash/provider/model/distance) for faster replay triage
  - expose enriched guidance through both model replay and proxy recorded-mode miss paths

### P1 Batch 9 (implemented in this PR)
P1 items: `33, 36`

- Add deterministic per-scenario timeout budget on top of global suite budget:
  - new CLI flag: `--scenario-budget` (ms)
  - runner computes effective per-scenario budget as `min(scenario-budget, remaining-suite-budget)`
  - timeout is enforced via scenario-scoped context deadline and reported as deterministic `scenario_timeout`
- Add causal failure taxonomy in scenario results:
  - `assertion_failure`
  - `infra_failure`
  - `timeout`
  - `budget_exhausted`
  - `nondeterminism_warning`
- Extend results schema/README to document `scenario_budget_ms`, per-scenario `budget_ms`, and `failure_category`.

### P1 Batch 10 (implemented in this PR)
P1 items: `40, 41`

- Add per-scenario environment freeze verification diagnostics:
  - introduce `determinism_env` trace event payload in TUT trace model
  - emit runtime verification report from Python SDK connect path (freeze time patch, locale application, timezone application)
  - evaluate report in runner and surface deterministic soft warnings (`nondeterminism.env`) when mismatch/missing
- Add explicit non-Python determinism guard story:
- when SDK capability negotiation reports a non-Python SDK and no runtime `determinism_env` report is available, emit deterministic warning that freeze verification parity is not yet implemented
- keep behavior non-blocking (soft diagnostic) to preserve backward compatibility while making gaps explicit

### P1 Batch 11 (implemented in this PR)
P1 item: `32`

- Add cryptographic evidence bundle signing for CI-uploaded artifacts:
  - new CLI command: `gauntlet sign-artifacts`
  - deterministic evidence manifest with file digest inventory (`evals/runs/evidence.manifest.json` by default)
  - Ed25519 signature metadata with key fingerprint for audit/verification
- Ensure generated CI workflows run:
  - `gauntlet scan-artifacts --dir evals`
  - `gauntlet sign-artifacts --dir evals/runs`
  before upload-artifact.
- Add tests for evidence manifest generation/signature verification and workflow step ordering.

### P1 Batch 12 (implemented in this PR)
P1 item: `37`

- Add run-history metadata to `results.json` without introducing dashboard coupling:
  - `history.recent`: newest-first window of prior run summaries for the same suite
  - `history.previous`: immediate prior run snapshot for fast diff context
  - `history.delta`: pass/fail/error/skipped + pass-rate deltas versus previous run
- Populate history metadata deterministically from local `evals/runs/*/results.json` artifacts (best-effort; malformed prior artifacts are skipped, not fatal).
- Extend results schema and tests to validate history ordering, suite filtering, delta math, and malformed-history tolerance.

### P1 Batch 13 (implemented in this PR)
P1 item: `50`

- Add default prompt-injection marker denylist detection to `gauntlet scan-artifacts`:
  - detector emits `prompt_injection_marker` findings for common override/jailbreak marker phrases in text and printable binary segments
  - enabled by default to harden recorded artifact uploads
- Add policy-level opt-out:
  - `redaction.prompt_injection_denylist: false` disables this detector for suites that intentionally store adversarial prompt text
  - policy schema updated with explicit boolean validation/docs
- Wire `scan-artifacts` to policy resolution so denylist behavior is governed by `evals/gauntlet.yml`.
- Add tests covering default-on detection, policy opt-out, and CLI behavior with opt-out policy.

### P1 Batch 14 (implemented in this PR)
P1 item: `34`

- Add scenario-level TUT process resource limits in policy:
  - `tut.resource_limits.cpu_seconds`
  - `tut.resource_limits.memory_mb`
  - `tut.resource_limits.open_files`
- Enforce limits per scenario process launch in CLI and HTTP adapters using deterministic wrapper semantics.
- Keep limits opt-in and backward compatible (unset means no resource cap changes).
- Extend policy schema and tests for parsing/validation, plus adapter tests for wrapper construction and config round-trip.

### P1 Batch 15 (implemented in this PR)
P1 item: `35`

- Add optional hostile-payload runtime guardrails via policy:
  - `tut.guardrails.hostile_payload` (linux-only enforcement)
  - `tut.guardrails.max_processes` (RLIMIT_NPROC cap, defaulted deterministically when enabled)
- Enforce guardrails in TUT adapter launch path:
  - linux: namespace-based process isolation (`unshare` PID/IPC/UTS + mount proc)
  - deterministic hard error on unsupported platforms or missing runtime support when explicitly enabled
- Preserve backward compatibility by keeping guardrails opt-in.
- Extend schema, policy parsing, run setup wiring, and adapter tests for guardrail config and wrapper behavior.

### P1 Batch 16 (implemented in this PR)
P1 item: `44`

- Add deterministic replay snapshot tests to guard canonical request hashing and replay lockfile index stability.
- Snapshot fixtures include both model and tool replay entries with fixed timestamps and deterministic inputs.
- Explicitly validate a cross-platform target matrix (`linux/darwin/windows` x `amd64/arm64`) against a single expected snapshot digest.
- Normalize replay lockfile `fixtures_dir` to forward slashes for path-separator stability across OS variants.

## P0 dependency graph
Legend: `A -> B` means A should land before B.

- `P0-1/2 (assertion policy runtime enforcement)` -> `P0-19 (baseline-change approval checks)`
- `P0-3 (single-fault tools+DB)` -> `P0-18 (baseline rollback automation)`
- `P0-4 (socket egress verification)` -> `P0-5 (mandatory PR CI egress self-test)`
- `P0-5` -> `P0-15 (untrusted CI live-mode hard guard)`
- `P0-10 (proxy fail-fast root-cause categories)` -> `P0-15`
- `P0-6 (migrate-fixtures command core)` -> `P0-7 (dry-run report + signed migration manifest)`
- `P0-6` -> `P0-14 (deterministic replay lockfile)`
- `P0-11 (fixture integrity checks)` -> `P0-20 (fixture poisoning defenses / trusted recorder identity)`
- `P0-12 (suite/scenario fixture binding)` -> `P0-13 (collision/malformed canonical replay hard-stop)`
- `P0-11/12/13` -> `P0-14`
- `P0-16 (fixture provenance capture)` -> `P0-20`
- `P0-17 (deterministic model-response schema validation)` -> `P0-6/7` (migration must preserve schema contract)
- `P0-8 (workflow scan-artifacts before upload)` is independent and can land with any early batch.
- `P0-18 (baseline rollback strategy)` -> `P0-19 (approval policy enforcement)`

## Batch plan

### Batch 1 (implemented in this PR)
P0 items: `1, 2, 3, 4, 5, 8, 10, 15`

- Runtime: enforce policy-defined `assertions.hard_gates` / `assertions.soft_signals` and reject invalid assertion policy keys.
- Runtime: single-fault validation counts both tools and databases.
- Runtime: replace DNS egress canary with outbound socket probe and explicit pre-scenario egress self-test in `pr_ci` / `fork_pr`.
- CI generation: enforce `gauntlet scan-artifacts` before artifact upload.
- Runtime security: deterministic proxy startup root-cause categorization; hard-block non-recorded model mode in untrusted fork CI.

Acceptance criteria:
- Policy assertion mode is parsed, validated, and applied to pass/fail status computation.
- Unknown keys under `assertions:` fail policy load with explicit diagnostics.
- Multi-fault detection rejects tool+DB fault combinations when `chaos: false`.
- `pr_ci` run fails before scenario load when egress self-test is not blocked.
- Generated workflow includes scan step before upload.
- Proxy startup failures include root cause category (`port_clash`, `cert_issue`, `permission`, `unknown`).
- Fork/untrusted CI cannot run live or passthrough model mode even if flags attempt override.

Rollback notes:
- Revert `internal/policy` + `internal/runner` assertion-mode wiring to restore previous gate classification.
- Revert `internal/world/variant_policy.go` signature/logic if DB-inclusive single-fault validation is too strict for existing suites.
- Revert `internal/runner/egress.go` socket probe if environment-specific false negatives appear; temporarily pin to local mode in CI as mitigation.
- Revert workflow template scan step only if it causes unacceptable CI timing impact.

### Batch 2 (implemented in this PR)
P0 items: `6, 7, 16, 17`

- Implement `gauntlet migrate-fixtures` end-to-end.
- Add dry-run migration diff report and signed manifest.
- Capture fixture provenance metadata (commit, SDK/toolchain versions).
- Validate model responses against deterministic schema before fixture persistence.

Acceptance criteria:
- Migration command rewrites hashes deterministically across fixture corpus.
- Dry-run report and signed manifest are generated and verifiable.
- New fixtures persist provenance and schema validation status.

Rollback notes:
- Keep old fixtures + manifest snapshots; provide one-command restore to pre-migration hashes.

### Batch 3 (implemented in this PR)
P0 items: `11, 12, 13, 14`

- Add fixture integrity/tamper checks in replay.
- Bind replay fixtures to suite/scenario context.
- Hard-error on collision or malformed canonical request material.
- Commit deterministic replay lockfile (fixture index checksum).

Acceptance criteria:
- Replay refuses tampered or cross-suite fixtures.
- Lockfile checksum mismatch is deterministic hard failure.

Rollback notes:
- Allow temporary compatibility mode flag (`--replay-integrity=warn`) while regenerating lockfiles.

### Batch 4 (implemented in this PR)
P0 items: `18, 19`

- Add baseline rollback PR template and automation hooks.
- Enforce required approval policy for baseline-changing PRs through CI checks.

Acceptance criteria:
- Baseline-update PRs include machine-generated rollback plan.
- Merge blocked when required approval policy is unmet.

Rollback notes:
- Disable check requirement while keeping templates if org-wide branch protection needs phased rollout.

### Batch 5 (implemented in this PR)
P0 item: `20`

- Implement fixture poisoning defenses: fixture signatures + trusted recorder identity validation.

Acceptance criteria:
- Replay accepts only fixtures signed by trusted identity/policy.
- Signature verification failures are deterministic hard errors.

Rollback notes:
- Keep verification in report-only mode for one release if trust bootstrap blocks adoption.
