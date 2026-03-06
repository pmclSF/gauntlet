# Docket Tags

Use docket tags to identify the most likely failure class and remediation path.

| Tag | Meaning | Typical Cause | How to Fix |
|-----|---------|---------------|------------|
| `fixture.miss` | A required fixture was not found | Tool/model call not recorded for this canonical request | Run `gauntlet record` for the scenario/suite, commit the new fixture |
| `fixture.integrity` | Fixture failed lockfile/signature integrity checks | Fixture tampering, stale lockfile, or signature mismatch | Regenerate lockfile with `gauntlet lock-fixtures`; re-sign fixtures in trusted context |
| `tut.exit_nonzero` | Agent process exited non-zero | Runtime crash, startup failure, or sandbox/egress wrapper failure | Inspect `pr_output.json` stderr, reproduce locally, fix crash/permissions |
| `input.malformed` | Scenario input was malformed | Invalid shape/types in `input` payload | Correct scenario YAML input fields to match expected schema |
| `input.schema_drift` | Input no longer matches expected schema | Upstream contract changed without scenario updates | Update scenario schema/input intentionally and baseline if expected |
| `tool.args_invalid` | Tool call args violated invariant or format | Planner emitted wrong/missing tool arguments | Fix planner/tool argument construction |
| `tool.timeout_retrycap` | Tool retries exceeded cap | Timeout path loops excessively | Add bounded retry/backoff logic |
| `tool.forbidden` | Forbidden tool was called | Agent violated scenario tool policy | Add planner/tool allowlist checks for that path |
| `tool.network_escape` | Tool attempted blocked network egress | Non-hermetic network call in restricted mode | Stub or fixture the call; remove live egress in PR mode |
| `rag.injection` | RAG/tool content triggered injection signal | Unsafe prompt/tool-output handling | Add sanitization/grounding guardrails before response synthesis |
| `planner.retry_storm` | Planner entered retry storm | Control-flow loop without stop condition | Add hard retry caps and terminal fallback |
| `planner.premature_finalize` | Agent finished before required sequence completed | Planner terminated early | Ensure required tool chain executes before finalize |
| `model.network_escape` | Model call bypassed replay controls | Direct model egress path outside proxy/fixtures | Route model traffic through Gauntlet proxy and recorded fixtures |
| `output.invalid_json` | Output payload not valid JSON | TUT emitted invalid/partial JSON | Ensure stable JSON output contract and error handling |
| `output.schema_mismatch` | Output did not satisfy schema | Missing fields or wrong types in response | Update agent output shape or schema intentionally |
| `output.sensitive_leak` | Sensitive content detected in output | Prompt/tool output leaked secrets/PII | Add redaction/filtering and tighten prompt/tool handling |
| `output.ungrounded` | Output claims not derivable from world/fixtures | Hallucinated or ungrounded response content | Improve grounding constraints and citation/use-of-tool logic |
| `unknown` | No stronger failure class matched | Novel or ambiguous failure mode | Inspect full artifact, add/extend classifier if recurring |
