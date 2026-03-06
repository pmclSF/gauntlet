package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

// handleRecorded looks up a fixture by canonical hash and returns the recorded
// response. Returns the fixture's original HTTP status code, defaulting to 200
// for backward compatibility with fixtures that predate status code recording.
func (p *Proxy) handleRecorded(normalizer providers.ProviderNormalizer, canonical *providers.CanonicalRequest, canonicalBytes []byte, hash string, start time.Time) ([]byte, int, error) {
	f, err := p.Store.GetModelFixture(hash)
	if err != nil {
		return nil, 0, err
	}
	if f == nil {
		candidates, _ := p.Store.NearestModelFixtureCandidates(canonical.ProviderFamily, canonical.Model, hash, 3)
		modelVersionHint := p.modelVersionHint(canonicalBytes, canonical.Model, candidates)
		return nil, 0, &fixture.ErrFixtureMiss{
			FixtureType:      "model:" + canonical.Model,
			ProviderFamily:   canonical.ProviderFamily,
			Model:            canonical.Model,
			CanonicalHash:    hash,
			CanonicalJSON:    string(canonicalBytes),
			RecordCmd:        "GAUNTLET_MODEL_MODE=live gauntlet record --suite smoke",
			Candidates:       candidates,
			ModelVersionHint: modelVersionHint,
		}
	}
	normalizedResponse, err := normalizer.NormalizeResponseForFixture(f.Response)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to normalize recorded response: %w", err)
	}
	promptTokens, completionTokens := normalizer.ExtractUsage(normalizedResponse)

	p.recordTrace(TraceEntry{
		Timestamp:        start,
		ProviderFamily:   canonical.ProviderFamily,
		Model:            canonical.Model,
		CanonicalHash:    hash,
		FixtureHit:       true,
		DurationMs:       int(time.Since(start).Milliseconds()),
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	})

	// Replay the recorded status code; default to 200 for fixtures that
	// predate status code recording.
	statusCode := f.ResponseCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	return normalizedResponse, statusCode, nil
}

func (p *Proxy) modelVersionHint(requestedCanonical []byte, requestedModel string, candidates []fixture.FixtureMissCandidate) string {
	recordSuite := strings.TrimSpace(p.Suite)
	if recordSuite == "" {
		recordSuite = "smoke"
	}
	requestedModel = strings.TrimSpace(requestedModel)

	for _, candidate := range candidates {
		recordedModel := strings.TrimSpace(candidate.Model)
		if recordedModel == "" || strings.EqualFold(recordedModel, requestedModel) {
			continue
		}
		recordedFixture, err := p.Store.GetModelFixture(candidate.CanonicalHash)
		if err != nil || recordedFixture == nil || len(recordedFixture.CanonicalRequest) == 0 {
			continue
		}
		match, cmpErr := canonicalEquivalentIgnoringModel(requestedCanonical, recordedFixture.CanonicalRequest)
		if cmpErr != nil || !match {
			continue
		}
		return fmt.Sprintf(
			"may be a model version change: recorded with %s, requesting %s. Run: gauntlet record --suite %s",
			recordedModel,
			requestedModel,
			recordSuite,
		)
	}
	return ""
}

func canonicalEquivalentIgnoringModel(leftCanonical, rightCanonical []byte) (bool, error) {
	var left map[string]interface{}
	if err := json.Unmarshal(leftCanonical, &left); err != nil {
		return false, err
	}
	var right map[string]interface{}
	if err := json.Unmarshal(rightCanonical, &right); err != nil {
		return false, err
	}
	delete(left, "model")
	delete(right, "model")
	return reflect.DeepEqual(left, right), nil
}
