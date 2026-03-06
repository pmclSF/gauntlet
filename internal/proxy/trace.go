package proxy

import "time"

// TraceWriter receives trace events for intercepted calls.
type TraceWriter interface {
	WriteTrace(event TraceEntry)
}

// TraceEntry is a single intercepted call record.
type TraceEntry struct {
	Timestamp        time.Time `json:"timestamp"`
	ProviderFamily   string    `json:"provider_family"`
	Model            string    `json:"model"`
	CanonicalHash    string    `json:"canonical_hash"`
	FixtureHit       bool      `json:"fixture_hit"`
	DurationMs       int       `json:"duration_ms"`
	PromptTokens     int       `json:"prompt_tokens,omitempty"`
	CompletionTokens int       `json:"completion_tokens,omitempty"`
}

// Traces returns all recorded trace entries.
func (p *Proxy) Traces() []TraceEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]TraceEntry{}, p.traces...)
}

func (p *Proxy) recordTrace(entry TraceEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.traces = append(p.traces, entry)
}
