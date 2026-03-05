# Variant Policy

Gauntlet enforces a single-fault variant policy by default: each scenario may exercise at most one non-nominal tool or database variant. This keeps test matrices manageable and failure causes isolated.

## What counts as a fault

A non-nominal variant is any tool state or database seed that is not the default "nominal" state. For example, a tool returning an error response or a database seeded with edge-case data.

## Multi-fault scenarios

To run scenarios with multiple simultaneous faults, set `chaos: true` in the scenario YAML:

```yaml
scenario: multi_fault_example
chaos: true
world:
  tools:
    lookup: error
    payment: timeout
```

Multi-fault chaos scenarios are accepted but logged with a warning, as they are outside the recommended single-fault testing discipline.

## Rationale

Single-fault isolation ensures that when a test fails, the root cause maps to exactly one variant change, making debugging straightforward.
