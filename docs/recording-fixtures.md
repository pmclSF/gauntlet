# Recording Fixtures

Gauntlet uses content-addressed fixtures to replay tool and model responses deterministically. When a fixture hash miss occurs during `gauntlet run`, the runner fails with an error indicating which tool call or model request has no recorded response.

## Recording new fixtures

```bash
gauntlet record --suite <suite-name>
```

This runs your agent against live APIs while capturing all tool and model responses as fixtures. Fixtures are stored in `evals/fixtures/tools/<tool>/<hash>.json` and `evals/fixtures/models/<hash>.json`.

## When to re-record

Re-record fixtures when:
- You add a new tool or modify tool input schemas
- You change the model prompt in ways that alter the tool-call sequence
- A fixture hash miss error occurs in CI

## Fixture integrity

Fixtures are hashed using SHA-256 over canonical request representations. The `gauntlet migrate-fixtures` command can recompute hashes if the canonicalization algorithm changes between versions.
