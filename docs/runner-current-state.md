# Runner Current State (Pre-Refactor)

## File Layout

```
internal/runner/
  runner.go        (970 lines) — monolithic orchestrator
  budget.go        (43 lines)  — BudgetEnforcer
  egress.go        (103 lines) — network egress self-test
  runner_test.go   — test file
  egress_test.go   — test file
```

## Control Flow: Runner.Run()

```
1. Egress self-test (pr_ci/fork_pr modes)
2. Path resolution (evals, suite, tools, DB, baseline dirs)
3. Scenario loading (scenario.LoadSuite)
4. Scenario filtering (by name)
5. World assembly (world.Assemble + tool ref validation)
6. Budget initialization (suite + per-scenario)
7. RunResult initialization
8. Per-scenario loop:
   a. Budget exhaustion check -> skip
   b. runScenario() -> scenarioExecution
   c. Failure category inference
   d. Aggregate counts
   e. FailFast break
9. Output directory creation
10. Artifact writing (results.json, summary.md, per-failure bundles)
11. Return RunResult
```

## Responsibility Map

| Cluster              | Lines  | Location                |
|----------------------|--------|-------------------------|
| Config/types         | 31-67  | runner.go top            |
| Run() orchestration  | 81-243 | runner.go                |
| runScenario()        | 245-453| runner.go                |
| Egress self-test     | 486-495| runner.go + egress.go    |
| Docket detection     | 518-556| runner.go                |
| World validation     | 584-615| runner.go                |
| Capability diags     | 635-702| runner.go                |
| Env freeze diags     | 704-782| runner.go                |
| TUT config building  | 836-850| runner.go                |
| DB preparation       | 909-939| runner.go                |
| Env/mode helpers     | 852-907| runner.go                |
| Budget management    | all    | budget.go                |
| Egress probing       | all    | egress.go                |

## Candidate Extraction Boundaries

1. **config.go** — Config struct, validation, NewRunner
2. **modes.go** — modeRequiresBlockedEgress, mode-specific defaults
3. **suite.go** — scenario loading, filtering, path resolution
4. **world.go** — world assembly, validateWorldToolRefs, sorted helpers
5. **tut.go** — buildTUTConfig, stripSensitiveEnv, isSensitiveEnvKey, env helpers
6. **scenario.go** — runScenario, scenarioExecution struct, timeout helpers
7. **baseline.go** — baseline loading, buildToolState, getToolSequence, getForbiddenContent, getRequiredFields
8. **assertions.go** — assertion context assembly, enforceAssertionMode
9. **diagnostics.go** — adapterCapabilityDiagnostics, environmentFreezeDiagnostics, time/locale helpers
10. **artifacts.go** — output dir creation, results/summary/bundle writing
11. **cleanup.go** — DB cleanup, TUT stop (currently inline defers)

## Known Risks

- runScenario() is 208 lines with deeply nested control flow
- Cleanup is via inline defers, not explicit cleanup functions
- Error handling mixes infrastructure errors with assertion failures
- Budget logic is split between Run() and budget.go
- Mode-specific behavior is scattered across multiple functions

## Dependencies

assertions, baseline, determinism, docket, output, scenario, tut, world
