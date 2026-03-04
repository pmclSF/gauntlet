// Package scaffold generates wrapper code that bridges discovered framework
// tools to @gauntlet.tool, plus a CLI adapter and world definitions.
package scaffold

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/gauntlet-dev/gauntlet/internal/discovery"
	"gopkg.in/yaml.v3"
)

//go:embed ast_extract.py
var astExtractScript string

// Config configures scaffold generation.
type Config struct {
	RootDir   string
	EvalsDir  string
	Proposals []discovery.Proposal
	Overwrite bool
}

// Result reports what was generated.
type Result struct {
	WrapperPath   string
	AdapterPath   string
	WorldFiles    []string
	SkippedFiles  []string
}

// ToolSignature is extracted by the Python AST script.
type ToolSignature struct {
	Name          string          `json:"name"`
	FuncName      string          `json:"func_name"`
	File          string          `json:"file"`
	Line          int             `json:"line"`
	IsAsync       bool            `json:"is_async"`
	Params        []ParamInfo     `json:"params"`
	ReturnType    string          `json:"return_type"`
	HasRunContext bool            `json:"has_run_context"`
	Framework     string          `json:"framework"`
	Docstring     string          `json:"docstring"`
}

// ParamInfo describes a function parameter.
type ParamInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Default string `json:"default,omitempty"`
}

// Generate creates scaffold files from discovered proposals.
func Generate(cfg Config) (*Result, error) {
	if len(cfg.Proposals) == 0 {
		return nil, fmt.Errorf("no proposals to scaffold; run 'gauntlet discover' first")
	}

	if cfg.EvalsDir == "" {
		cfg.EvalsDir = "evals"
	}

	result := &Result{}

	// Group proposals by file for AST extraction
	toolProposals := filterToolProposals(cfg.Proposals)
	if len(toolProposals) == 0 {
		return nil, fmt.Errorf("no tool proposals found to scaffold")
	}

	// Extract signatures via Python AST
	signatures, err := extractSignatures(cfg.RootDir, toolProposals)
	if err != nil {
		// Fall back to signature-less generation if Python AST fails
		fmt.Fprintf(os.Stderr, "Warning: AST extraction failed (%v), generating without signatures\n", err)
		signatures = fallbackSignatures(toolProposals)
	}

	// Generate gauntlet_tools.py wrapper
	wrapperPath := filepath.Join(cfg.RootDir, "gauntlet_tools.py")
	if !cfg.Overwrite && fileExists(wrapperPath) {
		result.SkippedFiles = append(result.SkippedFiles, wrapperPath)
	} else {
		if err := generateWrapper(wrapperPath, signatures); err != nil {
			return nil, fmt.Errorf("failed to generate wrapper: %w", err)
		}
		result.WrapperPath = wrapperPath
	}

	// Generate gauntlet_adapter.py CLI adapter
	adapterPath := filepath.Join(cfg.RootDir, "gauntlet_adapter.py")
	if !cfg.Overwrite && fileExists(adapterPath) {
		result.SkippedFiles = append(result.SkippedFiles, adapterPath)
	} else {
		if err := generateAdapter(adapterPath); err != nil {
			return nil, fmt.Errorf("failed to generate adapter: %w", err)
		}
		result.AdapterPath = adapterPath
	}

	// Generate world tool definitions
	worldDir := filepath.Join(cfg.EvalsDir, "world", "tools")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create world dir: %w", err)
	}
	for _, sig := range signatures {
		worldPath := filepath.Join(worldDir, sig.Name+".yaml")
		if !cfg.Overwrite && fileExists(worldPath) {
			result.SkippedFiles = append(result.SkippedFiles, worldPath)
			continue
		}
		if err := generateWorldDef(worldPath, sig); err != nil {
			return nil, fmt.Errorf("failed to generate world def for %s: %w", sig.Name, err)
		}
		result.WorldFiles = append(result.WorldFiles, worldPath)
	}

	return result, nil
}

func filterToolProposals(proposals []discovery.Proposal) []discovery.Proposal {
	var out []discovery.Proposal
	for _, p := range proposals {
		if p.Tool != "" {
			out = append(out, p)
		}
	}
	return out
}

type astInput struct {
	Files []astFileSpec `json:"files"`
}

type astFileSpec struct {
	Path  string   `json:"path"`
	Tools []string `json:"tools,omitempty"`
}

type astOutput struct {
	Tools []ToolSignature `json:"tools"`
}

// extractSignatures runs the embedded Python AST script to extract tool signatures.
func extractSignatures(rootDir string, proposals []discovery.Proposal) ([]ToolSignature, error) {
	// Group tools by file — we need the file from the proposal's description
	// Since proposals don't carry file path, we re-run discovery to get it
	engine := discovery.NewEngine(discovery.DiscoveryConfig{
		RootDir:    rootDir,
		PythonDirs: []string{"."},
	})
	allProposals, err := engine.Discover()
	if err != nil {
		return nil, fmt.Errorf("re-discovery failed: %w", err)
	}

	// Match proposals to get file paths from the full discovery results
	// For now, scan all Python files mentioned in proposals
	proposalNames := map[string]bool{}
	for _, p := range proposals {
		proposalNames[p.Tool] = true
	}

	// We need to find which files contain which tools
	// Use a simple approach: collect all .py files and let AST filter
	var pyFiles []string
	err = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			switch name {
			case ".git", "__pycache__", "node_modules", ".venv", "venv", ".eggs", "dist", "build":
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".py") {
			pyFiles = append(pyFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Build AST input — ask for all proposal tool names
	var toolNames []string
	for name := range proposalNames {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	input := astInput{}
	for _, f := range pyFiles {
		input.Files = append(input.Files, astFileSpec{
			Path:  f,
			Tools: toolNames,
		})
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	// Run Python AST extractor
	cmd := exec.Command("python3", "-c", astExtractScript)
	cmd.Stdin = strings.NewReader(string(inputJSON))
	cmd.Dir = rootDir
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("AST extractor failed: %s\n%s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("AST extractor failed: %w", err)
	}

	var result astOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse AST output: %w", err)
	}

	// Deduplicate by tool name, preferring earlier files
	seen := map[string]bool{}
	var sigs []ToolSignature
	for _, sig := range result.Tools {
		if seen[sig.Name] {
			continue
		}
		seen[sig.Name] = true
		// Make file paths relative to rootDir
		if sig.File != "" {
			if rel, err := filepath.Rel(rootDir, sig.File); err == nil {
				sig.File = rel
			}
		}
		sigs = append(sigs, sig)
	}

	// Also include proposals that AST didn't find (use fallback)
	for _, p := range proposals {
		if !seen[p.Tool] {
			sigs = append(sigs, ToolSignature{
				Name:      p.Tool,
				FuncName:  p.Tool,
				Framework: p.Framework,
			})
		}
	}

	// Use proposal framework info to augment (in case AST didn't categorize)
	_ = allProposals
	return sigs, nil
}

func fallbackSignatures(proposals []discovery.Proposal) []ToolSignature {
	var sigs []ToolSignature
	for _, p := range proposals {
		sigs = append(sigs, ToolSignature{
			Name:      p.Tool,
			FuncName:  p.Tool,
			Framework: p.Framework,
		})
	}
	return sigs
}

var wrapperTemplate = template.Must(template.New("wrapper").Parse(`# Auto-generated by gauntlet scaffold
# Review and customize this file, then commit it to your repo.
import gauntlet
{{ range . }}
{{ if .Docstring }}# {{ .Docstring }}{{ end }}
@gauntlet.tool(name="{{ .Name }}")
{{ if .IsAsync }}async {{ end }}def {{ .Name }}({{ .ParamString }}){{ .ReturnAnnotation }}:
    """Wraps {{ .FuncName }} from {{ .ModulePath }}"""
    {{ if .ImportPath }}from {{ .ImportPath }} import {{ .FuncName }} as _real_{{ .FuncName }}
    {{ end }}{{ if .IsAsync }}return await _real_{{ .FuncName }}({{ .CallArgs }}){{ else }}return _real_{{ .FuncName }}({{ .CallArgs }}){{ end }}
{{ end }}`))

type wrapperToolData struct {
	Name             string
	FuncName         string
	IsAsync          bool
	Docstring        string
	ParamString      string
	ReturnAnnotation string
	ImportPath       string
	ModulePath       string
	CallArgs         string
}

func generateWrapper(path string, signatures []ToolSignature) error {
	var data []wrapperToolData

	for _, sig := range signatures {
		d := wrapperToolData{
			Name:     sig.Name,
			FuncName: sig.FuncName,
			IsAsync:  sig.IsAsync,
		}

		if sig.Docstring != "" {
			// Take only first line and truncate if long
			ds := sig.Docstring
			if idx := strings.IndexByte(ds, '\n'); idx >= 0 {
				ds = ds[:idx]
			}
			ds = strings.TrimSpace(ds)
			if len(ds) > 80 {
				ds = ds[:77] + "..."
			}
			d.Docstring = ds
		}

		// Build param string (skip RunContext)
		var params []string
		var callArgs []string
		for _, p := range sig.Params {
			param := p.Name
			if p.Type != "" {
				param += ": " + p.Type
			}
			if p.Default != "" {
				param += " = " + p.Default
			}
			params = append(params, param)
			callArgs = append(callArgs, p.Name+"="+p.Name)
		}
		d.ParamString = strings.Join(params, ", ")
		d.CallArgs = strings.Join(callArgs, ", ")

		if sig.ReturnType != "" {
			d.ReturnAnnotation = " -> " + sig.ReturnType
		}

		// Compute import path from file
		if sig.File != "" {
			d.ImportPath = fileToImportPath(sig.File)
			d.ModulePath = sig.File
		} else {
			d.ModulePath = "(unknown)"
		}

		data = append(data, d)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return wrapperTemplate.Execute(f, data)
}

func fileToImportPath(filePath string) string {
	// Convert file path to Python import path
	rel := filePath
	rel = strings.TrimSuffix(rel, ".py")
	rel = strings.TrimSuffix(rel, "/__init__")
	rel = strings.ReplaceAll(rel, "/", ".")
	rel = strings.ReplaceAll(rel, "\\", ".")
	// Remove leading dots
	rel = strings.TrimLeft(rel, ".")
	return rel
}

var adapterTemplate = `# Auto-generated by gauntlet scaffold
# Wire your agent's entry point into the handle_request function below.
import sys
import json
import gauntlet

gauntlet.connect()

# Import wrapped tools (triggers @gauntlet.tool registration)
from gauntlet_tools import *  # noqa: F401,F403


def handle_request(input_data: dict) -> dict:
    """Process agent input and return output.

    Replace this with your actual agent invocation, e.g.:
        from myapp.agent import agent
        result = agent.run(input_data["messages"][-1]["content"])
        return {"response": result.output}
    """
    messages = input_data.get("messages", [])
    last_message = messages[-1]["content"] if messages else ""

    return {
        "response": f"TODO: wire agent to handle: {last_message}"
    }


def main():
    input_data = json.load(sys.stdin)
    result = handle_request(input_data)
    json.dump(result, sys.stdout)


if __name__ == "__main__":
    main()
`

func generateAdapter(path string) error {
	return os.WriteFile(path, []byte(adapterTemplate), 0o644)
}

type worldToolDef struct {
	Tool        string                   `yaml:"tool"`
	Description string                   `yaml:"description"`
	States      map[string]worldStateDef `yaml:"states"`
}

type worldStateDef struct {
	Description string `yaml:"description"`
}

func generateWorldDef(path string, sig ToolSignature) error {
	def := worldToolDef{
		Tool:        sig.Name,
		Description: fmt.Sprintf("Auto-discovered from %s (%s)", sig.Framework, sig.File),
		States: map[string]worldStateDef{
			"nominal": {Description: "Normal operation"},
		},
	}
	if sig.Docstring != "" {
		def.Description = sig.Docstring
	}

	data, err := yaml.Marshal(def)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
