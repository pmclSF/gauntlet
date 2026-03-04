# Gauntlet — Provider Support

## Interception model

All providers are intercepted via the Gauntlet proxy (localhost:7431).
No code changes required in the agent for interception to work.
SDK adapters provide optional richer traces.

## Supported providers

### openai_compatible family

Detected by: endpoint path contains /chat/completions

| Provider | Endpoint | Status |
|----------|----------|--------|
| OpenAI direct | api.openai.com | Full support |
| Azure OpenAI | *.openai.azure.com | Full support |
| Together AI | api.together.xyz | Full support |
| Fireworks AI | api.fireworks.ai | Full support |
| Groq | api.groq.com | Full support |
| Perplexity | api.perplexity.ai | Full support |
| Ollama | localhost:11434 | Full support (loopback interception) |
| vLLM | localhost:* | Full support (loopback interception) |
| LocalAI | localhost:* | Full support |
| LiteLLM proxy | localhost:* | Full support |
| llama.cpp server | localhost:* | Full support |

### anthropic family

Detected by: hostname == api.anthropic.com OR path contains /v1/messages
with Anthropic-style body structure

| Provider | Endpoint | Status |
|----------|----------|--------|
| Anthropic direct | api.anthropic.com | Full support |

### google family

Detected by: hostname contains googleapis.com

| Provider | Endpoint | Status |
|----------|----------|--------|
| Google AI Studio | generativelanguage.googleapis.com | Full support |
| Vertex AI | *.aiplatform.googleapis.com | Full support |

### bedrock_converse family

Detected by: hostname contains .amazonaws.com AND path contains /converse

| Provider | Endpoint | Status |
|----------|----------|--------|
| AWS Bedrock | *.bedrock-runtime.*.amazonaws.com | Full support |

### cohere family

Detected by: hostname == api.cohere.ai OR api.cohere.com

| Provider | Endpoint | Status |
|----------|----------|--------|
| Cohere direct | api.cohere.com | Full support |

### unknown family

Any provider not matching the above detection rules.
Behavior: raw request body stored as fixture with warning.

## Framework support

| Framework | Integration method | Level |
|-----------|-------------------|-------|
| OpenAI SDK (Python) | Proxy (automatic) + optional adapter | Best |
| Anthropic SDK (Python) | Proxy (automatic) + optional adapter | Best |
| LangChain | Proxy (automatic) + optional adapter | Best |
| LiteLLM | Proxy (automatic) + optional adapter | Best |
| LlamaIndex | Proxy (automatic) | Good |
| Haystack | Proxy (automatic) | Good |
| AutoGen | Proxy (automatic) | Good |
| CrewAI | Proxy (automatic) | Good |
| Any HTTP-based framework | Proxy (automatic) | Good |

## SDK parity roadmap

Gauntlet uses a versioned capability handshake so the runner can detect adapter
feature drift deterministically.

### Capability protocol (v1)

- Event type: `sdk_capabilities`
- Required fields: `protocol_version`, `sdk`, `adapters`
- Adapter fields: `enabled`, `patched`, optional `reason`
- Runner behavior:
  - emits soft `adapter_capabilities` diagnostics for missing handshake,
    unsupported protocol versions, or enabled-but-unpatched adapters

### Cross-language status matrix

| SDK | Status | Capability protocol | Adapter instrumentation |
|-----|--------|---------------------|-------------------------|
| Python | Implemented | v1 | OpenAI, Anthropic, LangChain |
| JS/TS | Planned | v1 target | Planned parity with Python adapters |
| Go | Planned | v1 target | Planned transport/callback parity |

### Near-term milestones

1. Implement JS/TS `connect()` parity with `sdk_capabilities` emission.
2. Implement Go SDK `connect()` parity with `sdk_capabilities` emission.
3. Add cross-SDK conformance tests validating identical v1 capability payload semantics.
