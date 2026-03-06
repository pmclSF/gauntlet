# Intentional Failure Example

This suite is designed to fail on purpose so developers can inspect a real
Gauntlet failure artifact before debugging their own regressions.

Run the suite:

```bash
cd examples/intentional-failure
../../bin/gauntlet run --suite smoke --auto-discover=false
```

Expected result:
- `planner_premature_finalize` fails
- failure artifact includes assertion mismatch and docket tag guidance
