# Gauntlet Improvement Backlog (Ranked by Severity)

This is a severity-ranked improvement list for Gauntlet. Items marked DONE have been implemented.

## Critical (P0)

1. ~~Enforce policy-defined `assertions.hard_gates` at runtime.~~ **DONE**
2. ~~Enforce policy-defined `assertions.soft_signals` at runtime and reject unknown assertion policy keys.~~ **DONE**
3. Fix single-fault policy enforcement to include **both tools and databases**; current variant enforcement only counts tools.
4. ~~Replace DNS-only egress check with explicit outbound socket verification.~~ **DONE**
5. ~~Add mandatory egress enforcement self-test in `pr_ci` mode before scenario execution.~~ **DONE**
6. ~~Implement fixture hash migration command (`migrate-fixtures`).~~ **DONE**
7. Add fixture migration dry-run report + signed migration manifest for auditability.
8. ~~Ensure CI-generated workflows always run `gauntlet scan-artifacts` before result upload.~~ **DONE**
9. Extend redaction scanner beyond extension allowlist to include binary-safe entropy and secret detectors.
10. ~~Introduce deterministic fail-fast when proxy cannot start with explicit root-cause categorization.~~ **DONE**
11. Add cryptographic integrity checks for fixture files (tamper detection) in replay mode.
12. Validate that replay fixtures are tied to scenario + suite context to prevent cross-suite fixture reuse.
13. Block replay when hash collisions or malformed canonical requests are detected.
14. ~~Add deterministic replay lockfile committed with baselines.~~ **DONE**
15. ~~Add hard guard that prevents `live` mode in fork/untrusted CI contexts.~~ **DONE**
16. Add provenance capture for fixture creation (commit SHA, toolchain versions, SDK versions).
17. Add deterministic model-response schema validation before storing fixtures.
18. ~~Add explicit rollback strategy for baseline updates (automated revert PR template).~~ **DONE**
19. ~~Add required approval policy for baseline-changing PRs via GitHub checks.~~ **DONE**
20. ~~Implement defense against fixture poisoning (fixture signatures and trusted recorder identity).~~ **DONE**

## High (P1)

21. Promote LangChain adapter from callback stubs to real event instrumentation.
22. Promote OpenAI adapter from no-op helper to transport-level interception.
23. Promote Anthropic adapter to robust hook behavior.
24. Add cross-language SDK parity plan (JS/TS/Go roadmap).
25. ~~Add versioned SDK capability negotiation.~~ **DONE**
26. Add policy schema validation on load with precise line/field diagnostics.
27. ~~Add "strict mode" for policy parsing that errors on unknown keys.~~ **DONE** (`--policy-strict`)
28. ~~Separate runner mode and model mode semantics in docs and CLI.~~ **DONE** (`--runner-mode`, `--model-mode`)
29. ~~Add `gauntlet doctor` command.~~ **DONE**
30. Replace regex-only sensitive leak checks with composable detectors (entropy + token formats).
31. Align threat model text with actual detector behavior.
32. Add evidence bundle signing for artifacts uploaded from CI.
33. ~~Add deterministic timeout budget per scenario.~~ **DONE** (`--scenario-budget`)
34. Add scenario-level resource limits (CPU/mem/open files).
35. Add syscall-level or cgroup-level guardrails for hostile payloads.
36. ~~Add detailed causal failure taxonomy (`failure_category` + culprit classification).~~ **DONE**
37. ~~Add trend/history metadata to results.~~ **DONE** (`history.previous`, `history.delta`)
38. ~~Add canonical error codes in CLI output for machine parsing.~~ **DONE** (`GAUNTLET_ERROR_CODE`)
39. ~~Improve fixture miss guidance with provider/model/hash + nearest candidates.~~ **DONE**
40. ~~Add environment freeze verification (timezone/locale) per scenario.~~ **DONE**
41. ~~Add non-Python determinism guard story.~~ **DONE** (explicit warnings for non-Python SDKs)
42. ~~Add explicit unsupported-feature warnings when adapters are no-op.~~ **DONE**
43. Add transport compatibility tests for streaming providers and SSE edge cases.
44. ~~Add replay determinism snapshot tests across OS variants.~~ **DONE**
45. ~~Add cert rotation and expiration checks for local CA assets.~~ **DONE** (doctor reports rotation warning)
46. ~~Add hardened proxy request parsing limits.~~ **DONE**
47. ~~Add controlled behavior for malformed JSON requests in proxy.~~ **DONE**
48. ~~Add better handling for HTTP/2 and websocket edge cases through proxy.~~ **DONE**
49. Add security review of proxy CA storage path permissions and multi-user host risks.
50. ~~Add default denylist for prompt injection markers in recorded artifacts.~~ **DONE**

## Medium (P2) — Roadmap

51. Add docs for auto-discovery stability guarantees and staleness hashing internals.
52. Add deterministic ordering for DB column creation and insert statements.
53. Add scenario lint command to detect anti-patterns.
54. Add policy lints for contradictory settings.
55. Add CLI subcommand for fixture pruning and orphan detection.
56. Add baseline diff visualizer with semantic JSON diff.
57. Expand redaction configuration to support hierarchical JSONPath.
58. Add test coverage for redaction false-positive/false-negative tradeoffs.
59. Add seeded chaos mode variants with deterministic matrix expansion.
60. Add dynamic scenario generation constraints (max combinations, exclusion rules).
61. Add replay cache warming and performance telemetry for large suites.
62. Add parallel execution mode with deterministic scheduling.
63. Add deterministic temporary directory naming for forensic reproducibility.
64. Add richer API endpoints for review UI filtering.
65. Add explicit backward compatibility guarantees for result schema versions.
66. Add benchmark suite for proxy throughput and fixture lookup latency.
67. Add release hardening checklist that includes threat model drift review.
68. Add package-level architecture decision records (ADRs).
69. Add clearer UI behavior when embedded assets are placeholder-only.
70. Add onboarding flow with sample deterministic smoke suite and golden outputs.
