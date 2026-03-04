package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	schemas "github.com/gauntlet-dev/gauntlet/schema"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

// ProposalArtifactVersion is the on-disk format version for proposals artifacts.
const ProposalArtifactVersion = 1

// ProposalArtifact is the versioned on-disk format for discovery output.
type ProposalArtifact struct {
	Version   int        `json:"version" yaml:"version"`
	Proposals []Proposal `json:"proposals" yaml:"proposals"`
}

// SaveProposals writes proposals to a versioned YAML artifact.
func SaveProposals(proposals []Proposal, path string) error {
	artifact := ProposalArtifact{
		Version:   ProposalArtifactVersion,
		Proposals: proposals,
	}
	if artifact.Proposals == nil {
		artifact.Proposals = []Proposal{}
	}
	if err := validateProposalArtifact(artifact); err != nil {
		return fmt.Errorf("invalid proposals artifact: %w", err)
	}

	data, err := yaml.Marshal(artifact)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadProposals reads proposals from a YAML artifact.
// Supports both the v1 versioned envelope and legacy top-level list format.
func LoadProposals(path string) ([]Proposal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	artifact, err := decodeProposalArtifact(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse proposals file %s: %w", path, err)
	}
	if artifact.Proposals == nil {
		artifact.Proposals = []Proposal{}
	}
	if err := validateProposalArtifact(artifact); err != nil {
		return nil, fmt.Errorf("invalid proposals file %s: %w", path, err)
	}
	return artifact.Proposals, nil
}

func decodeProposalArtifact(data []byte) (ProposalArtifact, error) {
	var root interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return ProposalArtifact{}, err
	}

	switch root.(type) {
	case []interface{}:
		// Legacy format: top-level list of proposals.
		var proposals []Proposal
		if err := yaml.Unmarshal(data, &proposals); err != nil {
			return ProposalArtifact{}, err
		}
		return ProposalArtifact{
			Version:   ProposalArtifactVersion,
			Proposals: proposals,
		}, nil
	case map[string]interface{}, map[interface{}]interface{}:
		var artifact ProposalArtifact
		if err := yaml.Unmarshal(data, &artifact); err != nil {
			return ProposalArtifact{}, err
		}
		return artifact, nil
	default:
		return ProposalArtifact{}, fmt.Errorf("expected YAML map or list, got %T", root)
	}
}

func validateProposalArtifact(artifact ProposalArtifact) error {
	docBytes, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("failed to marshal proposals artifact: %w", err)
	}

	schemaLoader := gojsonschema.NewBytesLoader(schemas.ProposalsSchema())
	docLoader := gojsonschema.NewBytesLoader(docBytes)
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}
	if result.Valid() {
		return nil
	}

	const maxErrors = 6
	parts := make([]string, 0, maxErrors+1)
	for i, ve := range result.Errors() {
		if i >= maxErrors {
			parts = append(parts, fmt.Sprintf("... and %d more", len(result.Errors())-maxErrors))
			break
		}
		field := ve.Field()
		if field == "(root)" {
			field = "root"
		}
		parts = append(parts, fmt.Sprintf("%s: %s", field, ve.Description()))
	}
	return errors.New(strings.Join(parts, "; "))
}
