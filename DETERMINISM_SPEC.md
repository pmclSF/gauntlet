# Gauntlet — Determinism Specification

## The guarantee

A Gauntlet run is deterministic if, given:
- identical scenario files
- identical world definitions
- identical agent code/prompt/planner

it produces byte-identical results on every execution, on every machine,
at any point in time.

## Freeze layers

### Layer 1: Network freeze (proxy)
All HTTP/HTTPS calls from TUT are intercepted.
Recorded mode: responses served from fixtures, real network unreachable.
Enforced at: proxy (localhost:7431) + OS-level egress block.
Any call that bypasses the proxy fails at egress.

### Layer 2: Tool freeze (@gauntlet.tool)
Decorated tools return fixture responses. Real function never called.
Undecorated tools that make network calls are caught by Layer 1.
Undecorated tools that access the filesystem are allowed (controlled by DB Layer).

### Layer 3: DB freeze (ephemeral DB)
Each scenario gets a fresh SQLite instance seeded from world definition.
Connection string injected as GAUNTLET_DB_<NAME>=sqlite:///tmp/gauntlet-<run>/<name>.db
Agent reads this env var and uses it instead of production DB.
Required change in agent: one line per DB client initialization.

### Layer 4: Time freeze
GAUNTLET_FREEZE_TIME=2025-01-15T10:00:00Z injected into TUT environment.
Python SDK patches at connect() time:
  datetime.datetime.now() -> returns frozen time
  datetime.datetime.utcnow() -> returns frozen time
  time.time() -> returns frozen epoch
  time.localtime() -> returns frozen local time

Violation detection: output contains timestamp differing from frozen time
by more than 1 second -> WARN: nondeterminism.time

### Layer 5: RNG freeze
GAUNTLET_RNG_SEED=42 injected into TUT environment.
Python SDK patches at connect() time:
  random.seed(42) called immediately
  numpy.random.seed(42) if numpy is available

Violation detection: output contains high-entropy string (Shannon entropy
> 3.5 bits/char for strings longer than 8 chars) not present in any
fixture -> WARN: nondeterminism.rng

### Layer 6: Locale and timezone freeze
GAUNTLET_LOCALE=en_US.UTF-8
GAUNTLET_TIMEZONE=UTC
Python SDK patches: locale.setlocale(), os.environ['TZ']

### Layer 7: Output canonicalization
Agent output JSON is canonicalized before comparison:
  Keys sorted lexicographically at every nesting level
  Arrays preserved in original order (semantically meaningful)
  Floats: no scientific notation for |x| < 1e15
  Unicode: NFC normalization
  No trailing whitespace
Both raw output and canonical output stored in artifact.
Comparison uses canonical form. Failure diffs show both.

## Prohibited nondeterminism sources

| Source | Detection | Severity |
|--------|-----------|----------|
| Network call bypassing proxy | OS egress block | ERROR (hard fail) |
| time.time() without SDK hook | Clock skew in output | WARN |
| uuid.uuid4() | Entropy check | WARN |
| os.urandom() | Entropy check | WARN |
| Env var read not in allowlist | Env access log | WARN |
| Filesystem write outside tmp | FS monitoring | ERROR |
| Parallel tool calls random order | Trace ordering check | WARN |

## Fixture canonicalization spec

Hash input = SHA-256(CanonicalJSON(NormalizedRequest))
Hash version = 1 (stored in fixture, used for migration)

Canonical JSON = keys sorted, floats normalized, arrays preserved, NFC unicode.

NormalizedRequest = provider-specific normalization -> canonical form.

Denylist (stripped before normalization, all providers):
  request_id, user, session_id, stream
  X-Request-ID, Date, User-Agent (headers)
  Authorization header value (presence noted, value stripped)
  x-api-key header value
  metadata.*, Any key ending in _id, _ts, _at, _timestamp

Fields NOT in denylist: preserved even if not in canonical form above.
Unknown fields are stored in "extra": {} in canonical form.
This prevents SDK upgrades from causing hash misses.

Replay lockfile index: deterministic sorted fixture index with SHA-256 checksum.
Path fields are normalized to forward slashes for cross-platform stability.
Snapshot tests pin canonical hashes + lockfile index digest across OS/arch variants.

## Migration

When hash_version increments:
  gauntlet migrate-fixtures --from-version N --to-version N+1

Recomputes hashes for all fixtures using new normalization rules.
Dry-run mode shows what would change before committing.
Run in nightly trusted context only.
