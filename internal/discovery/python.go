package discovery

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type pythonTool struct {
	ToolName  string
	FuncName  string
	FilePath  string
	Framework string // "gauntlet", "pydantic-ai", "openai-agents", "langchain"
}

type pythonImportRef struct {
	CurrentModule string
	RawModule     string
	Symbol        string
}

var (
	pythonImportGauntletRE   = regexp.MustCompile(`^\s*import\s+gauntlet(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?\s*$`)
	pythonFromGauntletImport = regexp.MustCompile(`^\s*from\s+gauntlet\s+import\s+(.+)$`)
	pythonFromImportRE       = regexp.MustCompile(`^\s*from\s+([A-Za-z0-9_\.]+)\s+import\s+(.+)$`)
	pythonDecoratorRE        = regexp.MustCompile(`^\s*@([A-Za-z_][A-Za-z0-9_\.]*)(?:\((.*)\))?\s*$`)
	pythonFuncRE             = regexp.MustCompile(`^\s*(?:async\s+def|def)\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	pythonNameArgRE          = regexp.MustCompile(`\bname\s*=\s*(?:"([^"]+)"|'([^']+)')`)
	pythonNameOverrideRE     = regexp.MustCompile(`\bname_override\s*=\s*(?:"([^"]+)"|'([^']+)')`)
	pythonPositionalNameRE   = regexp.MustCompile(`^\s*(?:"([^"]+)"|'([^']+)')`)
	pythonAgentAssignRE      = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*Agent\s*\(`)
)

// frameworkToolModules maps Python module paths to tool decorator names and their framework.
// NOTE: Keep in sync with internal/scaffold/ast_extract.py (framework_imports dict).
var frameworkToolModules = map[string][]frameworkDecoratorInfo{
	"agents":               {{Symbol: "function_tool", Framework: "openai-agents"}},
	"agents.tool":          {{Symbol: "function_tool", Framework: "openai-agents"}},
	"langchain_core.tools": {{Symbol: "tool", Framework: "langchain"}},
	"langchain.tools":      {{Symbol: "tool", Framework: "langchain"}},
}

type frameworkDecoratorInfo struct {
	Symbol    string
	Framework string
}

func (e *Engine) discoverFromPythonTools() ([]Proposal, error) {
	root := e.Config.RootDir
	if root == "" {
		root = "."
	}

	pythonDirs := e.Config.PythonDirs
	if len(pythonDirs) == 0 {
		pythonDirs = []string{"."}
	}

	fileSet := map[string]bool{}
	var files []string
	for _, dir := range pythonDirs {
		start := dir
		if !filepath.IsAbs(start) {
			start = filepath.Join(root, dir)
		}
		entries, err := collectPythonFiles(start)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, f := range entries {
			if fileSet[f] {
				continue
			}
			fileSet[f] = true
			files = append(files, f)
		}
	}

	type fileParse struct {
		tools   []pythonTool
		imports []pythonImportRef
	}
	parsed := make(map[string]fileParse, len(files))
	knownByModuleSymbol := map[string]pythonTool{}

	for _, f := range files {
		tools, imports, err := parsePythonFileForGauntletTools(root, f)
		if err != nil {
			continue
		}
		parsed[f] = fileParse{tools: tools, imports: imports}
		module := fileToModule(root, f)
		for _, t := range tools {
			knownByModuleSymbol[module+"."+t.FuncName] = t
		}
	}

	// Resolve "from x import y" references into tool names (best effort).
	for _, fp := range parsed {
		for _, imp := range fp.imports {
			module := resolveImportModule(imp.CurrentModule, imp.RawModule)
			if module == "" {
				continue
			}
			symbolKey := module + "." + imp.Symbol
			if _, ok := knownByModuleSymbol[symbolKey]; ok {
				continue
			}
			moduleFile, err := moduleToFile(root, module)
			if err != nil {
				continue
			}
			tools, _, err := parsePythonFileForGauntletTools(root, moduleFile)
			if err != nil {
				continue
			}
			for _, t := range tools {
				knownByModuleSymbol[module+"."+t.FuncName] = t
			}
		}
	}

	toolByName := map[string]pythonTool{}
	for _, t := range knownByModuleSymbol {
		if e.isExcluded(t.ToolName) {
			continue
		}
		if existing, ok := toolByName[t.ToolName]; ok {
			// Prefer lexicographically stable file path for deterministic output.
			if t.FilePath < existing.FilePath {
				toolByName[t.ToolName] = t
			}
			continue
		}
		toolByName[t.ToolName] = t
	}

	var names []string
	for name := range toolByName {
		names = append(names, name)
	}
	sort.Strings(names)

	var proposals []Proposal
	for _, name := range names {
		t := toolByName[name]
		framework := t.Framework
		if framework == "" {
			framework = "gauntlet"
		}
		tags := []string{"auto-discovered", "python-tool"}
		if framework != "gauntlet" {
			tags = append(tags, "framework-"+framework)
		}
		desc := fmt.Sprintf("Auto-discovered from Python @gauntlet.tool: %s", name)
		if framework != "gauntlet" {
			desc = fmt.Sprintf("Auto-discovered from %s tool decorator: %s", framework, name)
		}
		proposals = append(proposals, Proposal{
			ID:          fmt.Sprintf("disc-py-%s-nominal", sanitizeID(name)),
			Name:        fmt.Sprintf("%s_nominal", name),
			Description: desc,
			Tool:        name,
			Variant:     "nominal",
			Tags:        tags,
			Status:      "pending",
			Source:      "python_tool_ast",
			Framework:   framework,
		})
	}
	return proposals, nil
}

func collectPythonFiles(start string) ([]string, error) {
	info, err := os.Stat(start)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if strings.HasSuffix(start, ".py") {
			return []string{start}, nil
		}
		return nil, nil
	}

	var files []string
	err = filepath.WalkDir(start, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if shouldSkipPythonDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".py") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func shouldSkipPythonDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "__pycache__", "node_modules", ".venv", "venv":
		return true
	default:
		return strings.HasPrefix(name, ".mypy_cache")
	}
}

func parsePythonFileForGauntletTools(rootDir, filePath string) ([]pythonTool, []pythonImportRef, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	module := fileToModule(rootDir, filePath)
	gauntletAliases := map[string]bool{}
	toolAliases := map[string]bool{}

	// Framework detection state:
	// frameworkDecoratorAliases maps local alias → framework name
	// e.g. "function_tool" → "openai-agents", "tool" → "langchain"
	frameworkDecoratorAliases := map[string]string{}
	// agentVarNames tracks variables assigned via Agent(...) for @agent.tool detection
	agentVarNames := map[string]bool{}

	var pendingDecorators []string
	var tools []pythonTool
	var imports []pythonImportRef

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}

		// import gauntlet / import gauntlet as g
		if m := pythonImportGauntletRE.FindStringSubmatch(line); m != nil {
			alias := "gauntlet"
			if strings.TrimSpace(m[1]) != "" {
				alias = strings.TrimSpace(m[1])
			}
			gauntletAliases[alias] = true
			pendingDecorators = nil
			continue
		}

		// from gauntlet import tool
		if m := pythonFromGauntletImport.FindStringSubmatch(line); m != nil {
			for _, alias := range parseImportedNames(m[1]) {
				if alias.Original == "tool" {
					toolAliases[alias.Alias] = true
				}
			}
			pendingDecorators = nil
			continue
		}

		// from <module> import <symbols>
		if m := pythonFromImportRE.FindStringSubmatch(line); m != nil {
			rawModule := strings.TrimSpace(m[1])
			for _, imported := range parseImportedNames(m[2]) {
				imports = append(imports, pythonImportRef{
					CurrentModule: module,
					RawModule:     rawModule,
					Symbol:        imported.Original,
				})
				// Check if this import brings in a framework tool decorator
				if infos, ok := frameworkToolModules[rawModule]; ok {
					for _, info := range infos {
						if imported.Original == info.Symbol {
							frameworkDecoratorAliases[imported.Alias] = info.Framework
						}
					}
				}
			}
			pendingDecorators = nil
			continue
		}

		// Track Agent(...) assignments: agent = Agent(...)
		if m := pythonAgentAssignRE.FindStringSubmatch(line); m != nil {
			varName := strings.TrimSpace(m[1])
			agentVarNames[varName] = true
			pendingDecorators = nil
			continue
		}

		// @decorator or @decorator(args)
		if m := pythonDecoratorRE.FindStringSubmatch(line); m != nil {
			pendingDecorators = append(pendingDecorators, strings.TrimSpace(m[1])+"("+strings.TrimSpace(m[2])+")")
			continue
		}

		// def func_name(...) or async def func_name(...)
		if m := pythonFuncRE.FindStringSubmatch(line); m != nil {
			funcName := strings.TrimSpace(m[1])
			toolName := ""
			framework := ""
			for _, raw := range pendingDecorators {
				// Try gauntlet decorator first
				if name, ok := parseGauntletDecorator(raw, gauntletAliases, toolAliases); ok {
					if name == "" {
						toolName = funcName
					} else {
						toolName = name
					}
					framework = "gauntlet"
					break
				}
				// Try framework decorators
				if name, fw, ok := parseFrameworkDecorator(raw, frameworkDecoratorAliases, agentVarNames); ok {
					if name == "" {
						toolName = funcName
					} else {
						toolName = name
					}
					framework = fw
					break
				}
			}
			if toolName != "" {
				tools = append(tools, pythonTool{
					ToolName:  toolName,
					FuncName:  funcName,
					FilePath:  filePath,
					Framework: framework,
				})
			}
			pendingDecorators = nil
			continue
		}

		// Any other executable line clears decorator context.
		pendingDecorators = nil
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return tools, imports, nil
}

func parseGauntletDecorator(raw string, gauntletAliases, toolAliases map[string]bool) (string, bool) {
	decorator := raw
	args := ""
	if idx := strings.Index(raw, "("); idx >= 0 {
		decorator = strings.TrimSpace(raw[:idx])
		args = strings.TrimSuffix(raw[idx+1:], ")")
	}

	if strings.Contains(decorator, ".") {
		parts := strings.Split(decorator, ".")
		if len(parts) == 2 && parts[1] == "tool" && gauntletAliases[parts[0]] {
			return parseDecoratorToolName(args), true
		}
	}
	if toolAliases[decorator] {
		return parseDecoratorToolName(args), true
	}
	return "", false
}

// parseFrameworkDecorator checks if a raw decorator string matches a known
// framework tool pattern: @function_tool, @tool (langchain), @agent.tool, @agent.tool_plain.
func parseFrameworkDecorator(raw string, frameworkAliases map[string]string, agentVarNames map[string]bool) (string, string, bool) {
	decorator := raw
	args := ""
	if idx := strings.Index(raw, "("); idx >= 0 {
		decorator = strings.TrimSpace(raw[:idx])
		args = strings.TrimSuffix(raw[idx+1:], ")")
	}

	// Check standalone framework decorators: @function_tool, @tool
	if fw, ok := frameworkAliases[decorator]; ok {
		name := parseDecoratorToolNameWithOverride(args, fw)
		return name, fw, true
	}

	// Check PydanticAI agent instance decorators: @agent.tool, @agent.tool_plain
	if strings.Contains(decorator, ".") {
		parts := strings.Split(decorator, ".")
		if len(parts) == 2 && agentVarNames[parts[0]] {
			if parts[1] == "tool" || parts[1] == "tool_plain" {
				name := parseDecoratorToolName(args)
				return name, "pydantic-ai", true
			}
		}
	}

	return "", "", false
}

// parseDecoratorToolNameWithOverride extracts the tool name from decorator args,
// checking both name= and name_override= (used by OpenAI Agents SDK).
func parseDecoratorToolNameWithOverride(args, framework string) string {
	// Check name_override= first (OpenAI Agents SDK)
	if framework == "openai-agents" {
		if m := pythonNameOverrideRE.FindStringSubmatch(args); m != nil {
			if strings.TrimSpace(m[1]) != "" {
				return strings.TrimSpace(m[1])
			}
			return strings.TrimSpace(m[2])
		}
	}
	return parseDecoratorToolName(args)
}

func parseDecoratorToolName(args string) string {
	if m := pythonNameArgRE.FindStringSubmatch(args); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			return strings.TrimSpace(m[1])
		}
		return strings.TrimSpace(m[2])
	}
	if m := pythonPositionalNameRE.FindStringSubmatch(args); m != nil {
		if strings.TrimSpace(m[1]) != "" {
			return strings.TrimSpace(m[1])
		}
		return strings.TrimSpace(m[2])
	}
	return ""
}

type importedName struct {
	Original string
	Alias    string
}

func parseImportedNames(raw string) []importedName {
	parts := strings.Split(raw, ",")
	var out []importedName
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		sub := strings.Split(item, " as ")
		if len(sub) == 2 {
			out = append(out, importedName{
				Original: strings.TrimSpace(sub[0]),
				Alias:    strings.TrimSpace(sub[1]),
			})
			continue
		}
		out = append(out, importedName{
			Original: item,
			Alias:    item,
		})
	}
	return out
}

func fileToModule(rootDir, filePath string) string {
	rel, err := filepath.Rel(rootDir, filePath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".py")
	rel = strings.TrimSuffix(rel, "/__init__")
	rel = strings.Trim(rel, "/")
	return strings.ReplaceAll(rel, "/", ".")
}

func resolveImportModule(currentModule, rawModule string) string {
	if rawModule == "" {
		return ""
	}
	if !strings.HasPrefix(rawModule, ".") {
		return rawModule
	}

	dots := 0
	for dots < len(rawModule) && rawModule[dots] == '.' {
		dots++
	}
	suffix := strings.TrimPrefix(rawModule, strings.Repeat(".", dots))
	base := strings.Split(currentModule, ".")
	if len(base) == 0 {
		return strings.TrimPrefix(rawModule, ".")
	}
	if dots > len(base) {
		dots = len(base)
	}
	base = base[:len(base)-dots]
	if suffix != "" {
		base = append(base, strings.Split(suffix, ".")...)
	}
	return strings.Trim(strings.Join(base, "."), ".")
}

func moduleToFile(rootDir, module string) (string, error) {
	if module == "" {
		return "", fmt.Errorf("empty module")
	}
	p := filepath.Join(rootDir, filepath.FromSlash(strings.ReplaceAll(module, ".", "/"))+".py")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	pkgInit := filepath.Join(rootDir, filepath.FromSlash(strings.ReplaceAll(module, ".", "/")), "__init__.py")
	if _, err := os.Stat(pkgInit); err == nil {
		return pkgInit, nil
	}
	return "", fmt.Errorf("module %s not found under %s", module, rootDir)
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('-')
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "tool"
	}
	return out
}
