# Runner Architecture (Post-Refactor)

## File Layout

```
internal/runner/
  runner.go        (167 lines) — thin orchestrator: Run(), egress self-test, getCommit
  config.go        (58 lines)  — Config, Runner, scenarioExecution structs, NewRunner
  modes.go         (105 lines) — mode helpers, buildTUTConfig, env stripping, enforceAssertionMode
  suite.go         (61 lines)  — path resolution, scenario loading/filtering
  world.go         (136 lines) — world validation, tool state building, DB preparation
  scenario.go      (319 lines) — runScenario, timeout/failure helpers, baseline/docket helpers
  diagnostics.go   (198 lines) — adapter capability and env freeze diagnostics
  budget.go        (42 lines)  — BudgetEnforcer
  egress.go        (102 lines) — network egress self-test
  runner_test.go   — test file
  egress_test.go   — test file
```

## Control Flow: Runner.Run()

```
1. Egress self-test (pr_ci/fork_pr modes)          [runner.go]
2. Path resolution                                   [suite.go]
3. Scenario loading + filtering                      [suite.go]
4. World assembly + tool ref validation              [world.go]
5. Budget initialization                             [runner.go]
6. RunResult initialization                          [runner.go]
7. Per-scenario loop:                                [runner.go]
   a. Budget exhaustion check -> skip
   b. runScenario() -> scenarioExecution             [scenario.go]
   c. Failure category inference                     [scenario.go]
   d. Aggregate counts
   e. FailFast break
8. Output directory creation                         [runner.go]
9. Artifact writing (results.json, summary.md)       [runner.go]
10. Return RunResult
```

## Responsibility Map

| File            | Responsibility                                              |
|-----------------|-------------------------------------------------------------|
| runner.go       | Orchestration: Run(), egress self-test, artifact output     |
| config.go       | Config/Runner/scenarioExecution types, NewRunner            |
| modes.go        | Mode policy, TUT config building, env stripping, assertion mode |
| suite.go        | Path resolution, scenario loading and filtering             |
| world.go        | Tool/DB world validation, tool state, DB preparation        |
| scenario.go     | Per-scenario lifecycle, timeout, failure, baseline, docket  |
| diagnostics.go  | Adapter capability + environment freeze diagnostics         |
| budget.go       | Wall-clock budget enforcement                               |
| egress.go       | Network egress probe and classification                     |

## Dependencies

assertions, baseline, determinism, docket, output, scenario, tut, world
