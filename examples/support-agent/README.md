# Support Agent Example

A rule-based customer support agent demonstrating core Gauntlet features: tool decoration, fixture recording, and scenario testing.

## What it demonstrates

- `@gauntlet.tool` decoration for `order_lookup`, `web_search`, and `send_email`
- Tool world definitions with nominal, timeout, and error variants
- Database seeding for order state
- IO pair libraries for realistic inputs
- Six smoke scenarios covering nominal flow, timeouts, conflicting state, injection, and forbidden tools

## Prerequisites

- Gauntlet CLI installed
- Python 3.10+

## Run

```bash
cd examples/support-agent
pip install -e ../../sdk/python
gauntlet run --suite smoke
```
