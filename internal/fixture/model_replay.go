package fixture

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

// ModelReplay intercepts model calls (via proxy) and serves fixture responses.
type ModelReplay struct {
	Store  *Store
	Suite  string
	Traces []ModelCallTrace
}

// ModelCallTrace records a single model call for assertion evaluation.
type ModelCallTrace struct {
	ProviderFamily string          `json:"provider_family"`
	Model          string          `json:"model"`
	CanonicalHash  string          `json:"canonical_hash"`
	Response       json.RawMessage `json:"response"`
	FixtureUsed    string          `json:"fixture_used"`
	DurationMs     int             `json:"duration_ms"`
	Timestamp      time.Time       `json:"timestamp"`
}

// Replay looks up a fixture for the given model call.
// Returns the fixture response or ErrFixtureMiss.
// The real model endpoint is never called in recorded mode.
func (r *ModelReplay) Replay(cr *providers.CanonicalRequest) (*ModelFixture, error) {
	canonical, err := CanonicalizeRequest(cr)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize model request: %w", err)
	}

	hash := HashCanonical(canonical)

	fixture, err := r.Store.GetModelFixture(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to load model fixture: %w", err)
	}
	if fixture == nil {
		candidates, _ := r.Store.NearestModelFixtureCandidates(cr.ProviderFamily, cr.Model, hash, 3)
		return nil, &ErrFixtureMiss{
			FixtureType:    "model:" + cr.Model,
			ProviderFamily: cr.ProviderFamily,
			Model:          cr.Model,
			CanonicalHash:  hash,
			CanonicalJSON:  string(canonical),
			RecordCmd:      fmt.Sprintf("GAUNTLET_MODEL_MODE=live gauntlet record --suite %s", r.Suite),
			Candidates:     candidates,
		}
	}
	normalizer := providers.NormalizerForFamily(cr.ProviderFamily)
	normalizedResponse, err := normalizer.NormalizeResponseForFixture(fixture.Response)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize fixture response for %s: %w", cr.ProviderFamily, err)
	}
	fixtureCopy := *fixture
	fixtureCopy.Response = normalizedResponse

	// Record trace
	r.Traces = append(r.Traces, ModelCallTrace{
		ProviderFamily: cr.ProviderFamily,
		Model:          cr.Model,
		CanonicalHash:  hash,
		Response:       fixtureCopy.Response,
		FixtureUsed:    fixture.CanonicalHash,
		Timestamp:      time.Now(),
	})

	return &fixtureCopy, nil
}

// Reset clears recorded traces for a new scenario.
func (r *ModelReplay) Reset() {
	r.Traces = nil
}
