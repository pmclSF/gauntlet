// Package ci implements CI workflow generation and context detection.
package ci

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/gauntlet.yml.tmpl
var embeddedWorkflowTemplate string

//go:embed templates/evalgate-policy.yml.tmpl
var embeddedPolicyTemplate string

// EnableResult holds the results of the enable command.
type EnableResult struct {
	WorkflowPath string
	PolicyPath   string
	Framework    string
}

// Enable generates the CI workflow and policy files, and prints the onboarding checklist.
func Enable(projectDir string) (*EnableResult, error) {
	framework := DetectFramework(projectDir)

	// Create directories
	workflowDir := filepath.Join(projectDir, ".github", "workflows")
	evalsDir := filepath.Join(projectDir, "evals")
	smokeDir := filepath.Join(evalsDir, "smoke")

	for _, dir := range []string{workflowDir, smokeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Write workflow (prefer embedded template, fallback to inline constant)
	wfContent := embeddedWorkflowTemplate
	if wfContent == "" {
		wfContent = workflowTemplate
	}
	workflowPath := filepath.Join(workflowDir, "gauntlet.yml")
	if err := os.WriteFile(workflowPath, []byte(wfContent), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write workflow: %w", err)
	}

	// Write policy (prefer embedded template, fallback to inline constant)
	polContent := embeddedPolicyTemplate
	if polContent == "" {
		polContent = policyTemplate
	}
	policyPath := filepath.Join(evalsDir, "gauntlet.yml")
	if err := os.WriteFile(policyPath, []byte(polContent), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write policy: %w", err)
	}

	return &EnableResult{
		WorkflowPath: workflowPath,
		PolicyPath:   policyPath,
		Framework:    framework,
	}, nil
}

// PrintOnboardingChecklist prints framework-specific onboarding steps.
func PrintOnboardingChecklist(framework string) {
	fmt.Println("\nGauntlet enabled! Complete these steps to finish setup:")
	fmt.Println("1. Wrap each tool function with @gauntlet.tool")
	printToolSnippet(framework)
	fmt.Println("\n2. Add gauntlet.connect() to your agent entrypoint")
	printConnectSnippet(framework)
	fmt.Println("\n3. Add GAUNTLET_DB_X env var handling to DB initialization")
	printDBSnippet(framework)
	fmt.Println("\n4. Record fixtures from a trusted run:")
	fmt.Println("   GAUNTLET_MODEL_MODE=live gauntlet record --suite smoke")
	fmt.Println("\n5. Push to trigger your first Gauntlet run!")
}

func printToolSnippet(framework string) {
	switch framework {
	case "fastapi":
		fmt.Println(`
   import gauntlet

   @gauntlet.tool(name="order_lookup")
   async def lookup_order(order_id: str) -> dict:
       # This function is NOT called in PR CI — fixture returned instead
       async with httpx.AsyncClient() as client:
           resp = await client.get(f"https://api.example.com/orders/{order_id}")
           return resp.json()`)
	case "flask":
		fmt.Println(`
   import gauntlet

   @gauntlet.tool(name="order_lookup")
   def lookup_order(order_id: str) -> dict:
       # This function is NOT called in PR CI — fixture returned instead
       return requests.get(f"https://api.example.com/orders/{order_id}").json()`)
	case "langchain":
		fmt.Println(`
   import gauntlet
   from langchain.tools import tool

   @gauntlet.tool(name="order_lookup")
   @tool
   def lookup_order(order_id: str) -> dict:
       """Look up an order by ID."""
       return requests.get(f"https://api.example.com/orders/{order_id}").json()`)
	default:
		fmt.Println(`
   import gauntlet

   @gauntlet.tool(name="order_lookup")
   def lookup_order(order_id: str) -> dict:
       # This function is NOT called in PR CI — fixture returned instead
       return requests.get(f"https://api.example.com/orders/{order_id}").json()`)
	}
}

func printConnectSnippet(framework string) {
	switch framework {
	case "fastapi":
		fmt.Println(`
   # At the top of your main.py
   import gauntlet
   gauntlet.connect()  # no-op if Gauntlet not running; safe in production

   from fastapi import FastAPI
   app = FastAPI()`)
	case "flask":
		fmt.Println(`
   # At the top of your app.py
   import gauntlet
   gauntlet.connect()  # no-op if Gauntlet not running; safe in production

   from flask import Flask
   app = Flask(__name__)`)
	default:
		fmt.Println(`
   # At the top of your agent entrypoint
   import gauntlet
   gauntlet.connect()  # no-op if Gauntlet not running; safe in production`)
	}
}

func printDBSnippet(framework string) {
	switch framework {
	case "fastapi":
		fmt.Println(`
   import os
   DATABASE_URL = os.environ.get("GAUNTLET_DB_ORDERS", "sqlite:///./orders.db")

   # In FastAPI dependency:
   from sqlalchemy import create_engine
   engine = create_engine(DATABASE_URL)`)
	default:
		fmt.Println(`
   import os
   DATABASE_URL = os.environ.get("GAUNTLET_DB_ORDERS", "sqlite:///./orders.db")

   import sqlite3
   conn = sqlite3.connect(DATABASE_URL.replace("sqlite:///", ""))`)
	}
}

// DetectFramework attempts to detect the Python framework used in the project.
func DetectFramework(projectDir string) string {
	// Check requirements.txt
	reqPath := filepath.Join(projectDir, "requirements.txt")
	if data, err := os.ReadFile(reqPath); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "fastapi") {
			return "fastapi"
		}
		if strings.Contains(content, "flask") {
			return "flask"
		}
		if strings.Contains(content, "langchain") {
			return "langchain"
		}
	}

	// Check pyproject.toml
	pyprojectPath := filepath.Join(projectDir, "pyproject.toml")
	if data, err := os.ReadFile(pyprojectPath); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "fastapi") {
			return "fastapi"
		}
		if strings.Contains(content, "flask") {
			return "flask"
		}
		if strings.Contains(content, "langchain") {
			return "langchain"
		}
	}

	return "generic"
}

const workflowTemplate = `name: Gauntlet
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  gauntlet:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@692973e3d937129bcbf40652eb9f2f61becf3332 # v4.1.7
        with:
          persist-credentials: false

      - uses: actions/setup-go@479c797a328a0dfa73d811c5d9d2d8aa7b69b838 # v5.5.0
        with:
          go-version: '1.22'

      - uses: actions/setup-python@82c7e631bb3cdc910f68e0081d67478d79c6982d # v5.6.0
        with:
          python-version: '3.11'

      - name: Install Gauntlet
        run: |
          go install github.com/gauntlet-dev/gauntlet/cmd/gauntlet@latest

      - name: Install Python dependencies
        run: |
          pip install -r requirements.txt
          pip install gauntlet-sdk

      - name: Run Gauntlet smoke suite
        run: gauntlet run --suite smoke --mode pr_ci
        env:
          GAUNTLET_MODEL_MODE: recorded

      - name: Upload results
        if: always()
        uses: actions/upload-artifact@65462800fd760344b1a7b4382951275a0abb4808 # v4.3.0
        with:
          name: gauntlet-results
          path: evals/runs/
`

const policyTemplate = `# Gauntlet policy — controls CI behavior
version: 1

suites:
  smoke:
    scenarios: "evals/smoke/*.yaml"
    budget_ms: 300000  # 5 minutes
    mode: pr_ci

  full:
    scenarios: "evals/full/*.yaml"
    budget_ms: 900000  # 15 minutes
    mode: nightly

assertions:
  hard_gates:
    - output_schema
    - tool_sequence
    - tool_args_invariant
    - retry_cap
    - forbidden_tool

  soft_signals:
    - sensitive_leak
    - output_derivable

proxy:
  addr: "localhost:7431"

redaction:
  field_paths:
    - "**.api_key"
    - "**.password"
    - "**.token"
    - "**.secret"
  patterns:
    - "\\b\\d{4}[\\s-]?\\d{4}[\\s-]?\\d{4}[\\s-]?\\d{1,7}\\b"  # credit card
    - "\\b\\d{3}-\\d{2}-\\d{4}\\b"  # SSN
`
