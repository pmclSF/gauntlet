# Proxy Subsystem — Current State

## Architecture

The proxy is a local MITM HTTP/HTTPS proxy at `internal/proxy/`. All model
calls from the TUT are routed through it via `HTTPS_PROXY` env injection.

### File Layout

```
internal/proxy/
  proxy.go           # ~816 lines: everything (server, CONNECT, HTTP, intercept, replay, live, passthrough, helpers)
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

### CONNECT/TLS Path
1. `handleRequest` routes CONNECT → `handleConnect`
2. Hijack TCP, send 200 OK
3. If no CA: tunnel directly (`tunnelDirect`)
4. Issue MITM cert, start TLS server (HTTP/1.1 only)
5. `handleDecryptedConnection`: loop parsing HTTP/1.1 requests
6. Each request → `interceptRequest` → mode-specific handler

### Plain HTTP Path
1. `handleRequest` routes non-CONNECT → `handleHTTP`
2. Read body, extract hostname
3. → `interceptRequest` → mode-specific handler

### Interception Pipeline (`interceptRequest`)
1. Detect provider via `providers.Detect()`
2. Strip denylist headers
3. Normalize to `CanonicalRequest`
4. Canonicalize to JSON bytes
5. SHA-256 hash
6. Dispatch by mode: recorded/live/passthrough

### Mode Behaviors

**Recorded:** Lookup fixture by hash → return response or fixture-miss error.
Always returns HTTP 200.

**Live:** Strip stream flag → POST to upstream → normalize response → redact →
store fixture → return upstream status code.

**Passthrough:** POST to upstream → return response as-is.

## Known Structural Issues

### Bug 1: Method forced to POST (proxy.go:479, 544)
Both `handleLive` and `handlePassthrough` construct upstream requests with
hardcoded `"POST"`. The original HTTP method is lost.

### Bug 2: Query strings dropped (proxy.go:328, 382)
`interceptRequest` receives `req.URL.Path` which excludes the query string
(`req.URL.RawQuery`). Query parameters are not forwarded.

### Bug 3: Recorded replay always returns 200 (proxy.go:471)
`handleRecorded` returns `200` unconditionally. The fixture schema
(`ModelFixture`) has no field for response status code.

### Bug 4: No upstream transport timeouts (proxy.go:95-99)
`directClient` uses `&http.Transport{Proxy: nil}` with no dial, TLS, or
response timeouts. Live/passthrough requests can hang indefinitely.

### Bug 5: Unknown-provider requires JSON (providers/unknown.go:21)
`UnknownNormalizer.Normalize` calls `json.Unmarshal` and fails hard on
non-JSON or empty bodies. Should allow raw-body fallback for live/passthrough.

### Additional: Response Content-Type always JSON (proxy.go:342, 388)
All responses have `Content-Type: application/json` hardcoded.

## Test Coverage

68 test functions in proxy_test.go + 4 test files in providers/.

Key invariants tests rely on:
- Denylist header stripping produces identical hashes
- Unknown fields preserved in `Extra` map
- Image hashing consistent across providers and padding variants
- Recorded mode returns 200 (will change)
- Provider detection priority order
- Streaming normalization (Gemini NDJSON merge, OpenAI stream flag strip)
- TLS cert caching by hostname
- Request limits (body size, header size, request count, HTTP/2 rejection)
