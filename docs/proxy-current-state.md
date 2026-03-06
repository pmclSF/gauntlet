# Proxy Subsystem — Current State

## Architecture

The proxy is a local MITM HTTP/HTTPS proxy at `internal/proxy/`. All model
calls from the TUT are routed through it via `HTTPS_PROXY` env injection.

### File Layout

```
internal/proxy/
  proxy.go           # ~150 lines: types, Proxy struct, constructor, Start/Stop, EnvVars, handleRequest
  connect.go         # CONNECT/TLS MITM: handleConnect, handleDecryptedConnection, tunnelDirect
  http.go            # Plain HTTP handler: handleHTTP
  intercept.go       # Interception pipeline: interceptRequest, isMalformedJSONErr
  replay.go          # Recorded mode: handleRecorded, modelVersionHint, canonicalEquivalentIgnoringModel
  record.go          # Live mode: handleLive, stripStreamFlag
  passthrough.go     # Passthrough mode: handlePassthrough
  transport.go       # Upstream transport with explicit timeouts
  trace.go           # TraceWriter, TraceEntry, recordTrace, Traces
  errors.go          # proxyRequestError, error response helpers
  helpers.go         # headerMap, requestHeaderBytes, readRequestBody, limits, isWebSocketUpgrade
  tls.go             # CA management, cert issuance, rotation
  providers/
    detector.go      # Provider detection (priority-ordered)
    normalizer.go    # ProviderNormalizer interface + canonical types
    openai_compatible.go  # OpenAI/Azure/Ollama
    anthropic.go     # Anthropic
    google.go        # Gemini/Vertex
    bedrock.go       # AWS Bedrock
    cohere.go        # Cohere
    unknown.go       # Fallback for unrecognized providers
    usage.go         # Token extraction
```

## Request Lifecycle

### CONNECT/TLS Path (connect.go)
1. `handleRequest` routes CONNECT → `handleConnect`
2. Hijack TCP, send 200 OK
3. If no CA: tunnel directly (`tunnelDirect`)
4. Issue MITM cert, start TLS server (HTTP/1.1 only)
5. `handleDecryptedConnection`: loop parsing HTTP/1.1 requests
6. Each request → `interceptRequest` → mode-specific handler

### Plain HTTP Path (http.go)
1. `handleRequest` routes non-CONNECT → `handleHTTP`
2. Read body, extract hostname
3. → `interceptRequest` → mode-specific handler

### Interception Pipeline (intercept.go)
1. Detect provider via `providers.Detect()`
2. Strip denylist headers
3. Normalize to `CanonicalRequest`
4. Canonicalize to JSON bytes
5. SHA-256 hash
6. Dispatch by mode: recorded/live/passthrough

### Mode Behaviors

**Recorded (replay.go):** Lookup fixture by hash → return response with
recorded status code (defaults to 200 for backward compatibility with
pre-status-code fixtures).

**Live (record.go):** Strip stream flag → forward to upstream preserving
method and query string → normalize response → redact → store fixture
(including status code) → return upstream status code.

**Passthrough (passthrough.go):** Forward to upstream preserving method and
query string → return response as-is.

### Upstream Transport (transport.go)
All upstream requests go through `upstreamTransport` with explicit timeouts:
- Dial: 10s
- TLS handshake: 10s
- Response header: 30s
- Idle connection: 90s
- `Proxy: nil` to prevent routing through self

## Bugs Fixed in This Refactoring

1. **Method preserved** — upstream requests use original HTTP method (was hardcoded POST)
2. **Query strings preserved** — rawQuery forwarded to upstream (was dropped)
3. **Recorded status code replayed** — fixtures replay their ResponseCode (was always 200)
4. **Transport timeouts** — explicit dial/TLS/response timeouts prevent hanging (was none)

## Remaining Known Issues

### Bug 5: Unknown-provider requires JSON (providers/unknown.go)
`UnknownNormalizer.Normalize` calls `json.Unmarshal` and fails hard on
non-JSON or empty bodies. Should allow raw-body fallback for live/passthrough.

### Response Content-Type always JSON
All responses have `Content-Type: application/json` hardcoded.

## Test Coverage

37 proxy tests + 26 provider tests across:
- `proxy_test.go` — proxy integration tests
- `transport_test.go` — method/query/status preservation
- `replay_test.go` — status code replay, backward compat, fixture miss
- `tls_test.go` — cert caching, CA permissions, expiry
- `providers/*_test.go` — normalization, detection, streaming
