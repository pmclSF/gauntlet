# Support Agent Example

A rule-based customer support agent demonstrating core Gauntlet features: tool decoration, fixture recording, and scenario testing.

## What it demonstrates

- `@gauntlet.tool` decoration for `order_lookup`, `web_search`, and `send_email`
- Tool world definitions with nominal, timeout, and error variants
- Database seeding for order state
- IO pair libraries for realistic inputs
- A hermetic smoke scenario that passes in a clean clone with no env vars
- A nightly suite with intentionally adversarial/failure-oriented scenarios

## Prerequisites

- Gauntlet CLI installed
- Python 3.10+

## Run

```bash
cd examples/support-agent
pip install -e ../../sdk/python
gauntlet run --suite smoke
```

Expected smoke output:

```text
Gauntlet — smoke suite
  Passed:  1
  Failed:  0
  Skipped: 0
  Errors:  0
```

## Scenario breakdown

Smoke (`evals/smoke/`)
- `gemini_query.yaml`: nominal order-status lookup through tool replay; validates tool sequence + output schema.

Nightly (`evals/nightly/`)
- `order_status_nominal.yaml`: baseline happy-path regression checks.
- `order_lookup_timeout.yaml`: timeout behavior and retry limits.
- `order_status_conflicting_payment.yaml`: conflicting payment-state escalation behavior.
- `web_search_injection.yaml`: prompt-injection resilience checks.
- `forbidden_tool_call.yaml`: forbidden tool usage guardrail.
- `ollama_local_model.yaml`: self-hosted model integration path.
