# Gauntlet — Architecture

## System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        gauntlet run                              │
└────────────────────────────┬────────────────────────────────────┘
                             │
               ┌─────────────▼─────────────┐
               │         Runner             │
               │  - load scenarios          │
               │  - enforce budget          │
               │  - manage world state      │
               │  - start proxy             │
               └──────┬──────────┬──────────┘
                      │          │
         ┌────────────▼──┐  ┌───▼──────────────┐
         │   Scenario    │  │  World Assembler  │
         │   Loader      │  │                  │
         │               │  │  - tool states   │
         │  - parse YAML │  │  - DB seeds      │
         │  - validate   │  │  - single-fault  │
         │  - matrix     │  │    enforcement   │
         └───────────────┘  └──────────────────┘
                      │
         ┌────────────▼──────────────────────────┐
         │         Gauntlet Proxy                 │
         │         localhost:7431                 │
         │                                        │
         │  All TUT model calls routed here via   │
         │  HTTPS_PROXY env var injection         │
         │                                        │
         │  Recorded mode:                        │
         │    detect provider → normalize →       │
         │    hash → fixture lookup → respond     │
         │                                        │
         │  Live mode:                            │
         │    forward to real endpoint →          │
         │    record response as fixture          │
         └───────┬────────────────────────────────┘
                 │
         ┌───────▼──────────────┐
         │  Target Under Test   │
         │  (TUT)               │
         │                      │
         │  env injected:       │
         │  HTTPS_PROXY=:7431   │
         │  GAUNTLET_ENABLED=1  │
         │  GAUNTLET_DB_X=...   │
         │  GAUNTLET_FREEZE_... │
         │                      │
         │  Tools wrapped with  │
         │  @gauntlet.tool      │
         │  (optional but       │
         │   recommended)       │
         └───────┬──────────────┘
                 │
         ┌───────▼──────────────┐
         │  Assertion Engine    │
         │                      │
         │  Hard gates:         │
         │  - input_schema      │
         │  - tool_sequence     │
         │  - tool_args         │
         │  - retry_cap         │
         │  - output_schema     │
         │  - forbidden_tool    │
         │  - output_derivable  │
         │                      │
         │  Soft signals:       │
         │  - sensitive_leak    │
         └───────┬──────────────┘
                 │
         ┌───────▼──────────────┐
         │  Output Producer     │
         │                      │
         │  - results.json      │
         │  - summary.md        │
         │  - artifact bundles  │
         │  - culprit report    │
         └──────────────────────┘
```

## Provider-agnostic model interception

```
Agent process (any framework, any provider)
    │
    │  HTTPS_PROXY=http://localhost:7431 (injected by Gauntlet)
    │
    ▼
┌─────────────────────────────────────┐
│     Gauntlet Proxy (localhost:7431) │
│                                     │
│  1. Detect provider family          │
│     (from hostname + path + body)   │
│                                     │
│  2. Normalize to canonical form     │
│     (provider-agnostic internal     │
│      representation)                │
│                                     │
│  3. Hash canonical form (SHA-256)   │
│                                     │
│  4a. Recorded mode:                 │
│      fixture lookup → respond       │
│      miss → hard failure            │
│                                     │
│  4b. Live mode:                     │
│      forward → record → respond     │
└─────────────────────────────────────┘
```

## Canonical form (provider-agnostic)

```json
{
  "gauntlet_canonical_version": 1,
  "provider_family": "openai_compatible",
  "model": "gpt-4o-2024-11-20",
  "system": "You are a customer support agent.",
  "messages": [
    {"role": "user", "content": "What is the status of order ord-001?"}
  ],
  "tools": [
    {
      "name": "order_lookup",
      "description": "...",
      "parameters": {}
    }
  ],
  "sampling": {
    "temperature": 0,
    "max_tokens": 1000
  }
}
```

Tools array is sorted by `name` for stability.
Messages array order is preserved (semantically meaningful).
All other keys sorted lexicographically.

## Denylist canonicalization

Fields stripped from all provider requests before normalization:

```
request_id, user, session_id, stream, n (when 1)
X-Request-ID, Date, User-Agent, Authorization (header)
x-api-key (header — key value, not presence)
metadata.*, extra_headers.*, http_client_*
Any field whose name ends in: _id, _ts, _at, _timestamp
```

Unknown fields NOT in the denylist are PRESERVED.
This ensures SDK upgrades do not cause hash misses.

## Trust model

```
UNTRUSTED (fork PR)                TRUSTED (same-repo / nightly)
──────────────────────────         ──────────────────────────────
GAUNTLET_MODE=fork_pr              GAUNTLET_MODE=pr_ci or nightly
Replay-only                        Replay + live model calls (nightly)
No secrets                         Secrets scoped per workflow
Contract checks only               Contract + optional judge (v2)
Cannot update baselines            Nightly proposes baseline update PR
Network blocked for TUT            Network allowed for live model calls
Model API keys = empty string      Model API keys = real values
```

## Integration levels

| Level   | What it needs           | What Gauntlet can check                      |
|---------|------------------------|----------------------------------------------|
| Best    | HTTP + @gauntlet.tool  | Everything: tool traces, model traces, all assertions |
| Good    | HTTP only (no decorator) | Model traces via proxy, output assertions, schema |
| Minimal | CLI only (no changes)  | Exit code, budget enforcement, egress blocking |

## Determinism layers

1. Network freeze — proxy blocks all egress in recorded mode
2. Tool freeze — @gauntlet.tool short-circuits real calls
3. DB freeze — ephemeral SQLite seeded from world definition
4. Time freeze — GAUNTLET_FREEZE_TIME injected, SDK patches datetime
5. RNG freeze — GAUNTLET_RNG_SEED=42 injected, SDK patches random
6. Locale freeze — GAUNTLET_LOCALE + GAUNTLET_TIMEZONE injected
7. Output canonicalization — JSON sorted before comparison

## Auto-discovery pipeline

```
gauntlet run --suite smoke --auto-discover
  ├─ 1. Discover: scan tools, DBs, Python decorators → proposals
  ├─ 2. Load IO pairs (if present) → derived assertions
  ├─ 3. Load world definitions → available tool states, DB seeds
  ├─ 4. Materialize: proposals × world state → scenario YAML files
  ├─ 5. Runner loads generated scenarios from suite dir
  └─ 6. Normal execution: proxy → TUT → assertions → output
```

Generated scenarios live in `evals/<suite>/auto_*.yaml` with a
`# gauntlet:auto-generated` header. Manual scenarios in the same directory
cause auto-discovery to skip (use `--discover-force` to override).

Hash-based staleness detection: if tool/DB definitions and proposals
haven't changed, auto scenarios are not regenerated.

## Self-hosted model handling

For local model servers (Ollama, vLLM, llama.cpp, etc.):

The proxy intercepts calls to localhost:* and loopback addresses.
In recorded mode: calls to localhost:11434 (Ollama) return fixtures.
In live mode: calls to localhost:11434 are forwarded to the real server.
The model server must be running in live mode. In recorded mode, it need not exist.
