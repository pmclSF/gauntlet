package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFromPythonTools_ModuleClassAndMultipleDecorators(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "agent")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	code := `import gauntlet
from gauntlet import tool as gt

def retry(fn):
    return fn

@gauntlet.tool(name="order_lookup")
def lookup_order(order_id):
    return {"order_id": order_id}

class Billing:
    @retry
    @gauntlet.tool
    def payment_lookup(self, payment_id):
        return {"payment_id": payment_id}

@gt(name="cancel_order")
async def cancel(order_id):
    return {"ok": True}
`
	if err := os.WriteFile(filepath.Join(srcDir, "tools.py"), []byte(code), 0o644); err != nil {
		t.Fatalf("write tools.py: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:    root,
		PythonDirs: []string{"agent"},
	})

	proposals, err := engine.discoverFromPythonTools()
	if err != nil {
		t.Fatalf("discoverFromPythonTools: %v", err)
	}

	names := map[string]bool{}
	for _, p := range proposals {
		if p.Source != "python_tool_ast" {
			t.Fatalf("unexpected source %q", p.Source)
		}
		names[p.Tool] = true
	}
	if !names["order_lookup"] {
		t.Fatalf("missing order_lookup in proposals: %#v", proposals)
	}
	if !names["payment_lookup"] {
		t.Fatalf("missing payment_lookup in proposals: %#v", proposals)
	}
	if !names["cancel_order"] {
		t.Fatalf("missing cancel_order in proposals: %#v", proposals)
	}
}

func TestDiscoverFromPythonTools_ResolvesImportedToolModule(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg")
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "__init__.py"), []byte(""), 0o644); err != nil {
		t.Fatalf("write __init__: %v", err)
	}

	lib := `import gauntlet

@gauntlet.tool(name="inventory_lookup")
def lookup_inventory(sku):
    return {"sku": sku}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "tool_lib.py"), []byte(lib), 0o644); err != nil {
		t.Fatalf("write tool_lib.py: %v", err)
	}

	importer := `from pkg.tool_lib import lookup_inventory

def run():
    return lookup_inventory("sku-1")
`
	if err := os.WriteFile(filepath.Join(appDir, "agent.py"), []byte(importer), 0o644); err != nil {
		t.Fatalf("write agent.py: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:    root,
		PythonDirs: []string{"app"},
	})

	proposals, err := engine.discoverFromPythonTools()
	if err != nil {
		t.Fatalf("discoverFromPythonTools: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal from imported tool module, got %d (%#v)", len(proposals), proposals)
	}
	if proposals[0].Tool != "inventory_lookup" {
		t.Fatalf("tool = %q, want inventory_lookup", proposals[0].Tool)
	}
}

func TestDiscoverFromPythonTools_PydanticAI(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "agent")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Simulates real PydanticAI code with @agent.tool and @agent.tool_plain
	code := `from pydantic_ai import Agent, RunContext
from pydantic import BaseModel

class SupportOutput(BaseModel):
    support_advice: str
    block_card: bool
    risk: int

support_agent = Agent(
    "openai:gpt-4o",
    output_type=SupportOutput,
)

@support_agent.tool
async def customer_balance(ctx: RunContext[dict], customer_id: int) -> str:
    """Get the customer's current balance."""
    return f"${customer_id}.00"

@support_agent.tool(name="card_status")
async def get_card_status(ctx: RunContext[dict], customer_id: int) -> str:
    return "active"

@support_agent.tool_plain
def lookup_faq(question: str) -> str:
    return "See our help center."

@support_agent.tool_plain(name="support_hours")
def get_support_hours() -> str:
    return "9am-5pm"
`
	if err := os.WriteFile(filepath.Join(srcDir, "tools.py"), []byte(code), 0o644); err != nil {
		t.Fatalf("write tools.py: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:    root,
		PythonDirs: []string{"agent"},
	})

	proposals, err := engine.discoverFromPythonTools()
	if err != nil {
		t.Fatalf("discoverFromPythonTools: %v", err)
	}

	byName := map[string]Proposal{}
	for _, p := range proposals {
		byName[p.Tool] = p
	}

	// @support_agent.tool → func name as tool name
	if _, ok := byName["customer_balance"]; !ok {
		t.Fatalf("missing customer_balance in proposals: %v", proposals)
	}
	// @support_agent.tool(name="card_status") → explicit name
	if _, ok := byName["card_status"]; !ok {
		t.Fatalf("missing card_status in proposals: %v", proposals)
	}
	// @support_agent.tool_plain → func name
	if _, ok := byName["lookup_faq"]; !ok {
		t.Fatalf("missing lookup_faq in proposals: %v", proposals)
	}
	// @support_agent.tool_plain(name="support_hours") → explicit name
	if _, ok := byName["support_hours"]; !ok {
		t.Fatalf("missing support_hours in proposals: %v", proposals)
	}

	// Verify framework tagging
	for _, p := range proposals {
		if p.Framework != "pydantic-ai" {
			t.Fatalf("expected framework pydantic-ai for %q, got %q", p.Tool, p.Framework)
		}
	}
}

func TestDiscoverFromPythonTools_OpenAIAgents(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "agent")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Simulates real OpenAI Agents SDK code with @function_tool
	code := `from agents import Agent, function_tool
from pydantic import BaseModel

class Weather(BaseModel):
    city: str
    temperature: str
    conditions: str

@function_tool
def get_weather(city: str) -> Weather:
    """Get the weather for a given city."""
    return Weather(city=city, temperature="72F", conditions="Sunny")

@function_tool(name_override="fetch_forecast")
def get_forecast(city: str, days: int = 5) -> str:
    return f"Forecast for {city}: sunny for {days} days"

weather_agent = Agent(
    name="weather_agent",
    instructions="Help users with weather.",
    tools=[get_weather, get_forecast],
)
`
	if err := os.WriteFile(filepath.Join(srcDir, "tools.py"), []byte(code), 0o644); err != nil {
		t.Fatalf("write tools.py: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:    root,
		PythonDirs: []string{"agent"},
	})

	proposals, err := engine.discoverFromPythonTools()
	if err != nil {
		t.Fatalf("discoverFromPythonTools: %v", err)
	}

	byName := map[string]Proposal{}
	for _, p := range proposals {
		byName[p.Tool] = p
	}

	// @function_tool bare → func name
	if _, ok := byName["get_weather"]; !ok {
		t.Fatalf("missing get_weather in proposals: %v", proposals)
	}
	// @function_tool(name_override="fetch_forecast") → explicit name
	if _, ok := byName["fetch_forecast"]; !ok {
		t.Fatalf("missing fetch_forecast in proposals: %v", proposals)
	}

	for _, p := range proposals {
		if p.Framework != "openai-agents" {
			t.Fatalf("expected framework openai-agents for %q, got %q", p.Tool, p.Framework)
		}
	}
}

func TestDiscoverFromPythonTools_LangChain(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "agent")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	code := `from langchain_core.tools import tool

@tool
def search(query: str) -> str:
    """Search the web for information."""
    return f"Results for: {query}"

@tool
def calculator(expression: str) -> str:
    """Evaluate a math expression."""
    return str(eval(expression))
`
	if err := os.WriteFile(filepath.Join(srcDir, "tools.py"), []byte(code), 0o644); err != nil {
		t.Fatalf("write tools.py: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:    root,
		PythonDirs: []string{"agent"},
	})

	proposals, err := engine.discoverFromPythonTools()
	if err != nil {
		t.Fatalf("discoverFromPythonTools: %v", err)
	}

	byName := map[string]Proposal{}
	for _, p := range proposals {
		byName[p.Tool] = p
	}

	if _, ok := byName["search"]; !ok {
		t.Fatalf("missing search in proposals: %v", proposals)
	}
	if _, ok := byName["calculator"]; !ok {
		t.Fatalf("missing calculator in proposals: %v", proposals)
	}

	for _, p := range proposals {
		if p.Framework != "langchain" {
			t.Fatalf("expected framework langchain for %q, got %q", p.Tool, p.Framework)
		}
	}
}

func TestDiscoverFromPythonTools_MixedFrameworks(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "agent")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// File with both gauntlet and framework decorators
	code := `import gauntlet
from agents import function_tool

@gauntlet.tool(name="order_lookup")
def lookup_order(order_id: str) -> dict:
    return {"order_id": order_id}

@function_tool
def get_weather(city: str) -> str:
    return f"Weather for {city}"
`
	if err := os.WriteFile(filepath.Join(srcDir, "tools.py"), []byte(code), 0o644); err != nil {
		t.Fatalf("write tools.py: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:    root,
		PythonDirs: []string{"agent"},
	})

	proposals, err := engine.discoverFromPythonTools()
	if err != nil {
		t.Fatalf("discoverFromPythonTools: %v", err)
	}

	byName := map[string]Proposal{}
	for _, p := range proposals {
		byName[p.Tool] = p
	}

	if p, ok := byName["order_lookup"]; !ok {
		t.Fatalf("missing order_lookup")
	} else if p.Framework != "gauntlet" {
		t.Fatalf("order_lookup framework = %q, want gauntlet", p.Framework)
	}

	if p, ok := byName["get_weather"]; !ok {
		t.Fatalf("missing get_weather")
	} else if p.Framework != "openai-agents" {
		t.Fatalf("get_weather framework = %q, want openai-agents", p.Framework)
	}
}

func TestResolveImportModule(t *testing.T) {
	tests := []struct {
		current string
		raw     string
		want    string
	}{
		{"pkg.agent", "pkg.tools", "pkg.tools"},
		{"pkg.agent", ".tools", "pkg.tools"},
		{"pkg.sub.agent", "..tools", "pkg.tools"},
	}
	for _, tt := range tests {
		if got := resolveImportModule(tt.current, tt.raw); got != tt.want {
			t.Fatalf("resolveImportModule(%q, %q) = %q, want %q", tt.current, tt.raw, got, tt.want)
		}
	}
}
