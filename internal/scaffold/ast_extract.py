"""AST-based tool signature extractor for gauntlet scaffold.

Invoked by Go binary via subprocess. Receives JSON on stdin with file paths
and tool names, outputs JSON on stdout with function signatures.

Input format:
  {"files": [{"path": "agent/tools.py", "tools": ["get_weather", "calculator"]}]}

Output format:
  {"tools": [{"name": "get_weather", "func_name": "get_weather", ...}]}
"""
import ast
import json
import sys
from typing import Any, Dict, List, Optional, Set


# Known tool decorator patterns by framework
PYDANTIC_AI_TOOL_ATTRS = {"tool", "tool_plain"}
OPENAI_AGENTS_DECORATORS = {"function_tool"}
LANGCHAIN_DECORATORS = {"tool"}

# Known modules that provide Agent class
PYDANTIC_AI_MODULES = {"pydantic_ai", "pydantic_ai_slim"}


def classify_decorator(dec, agent_vars, framework_imports):
    """Classify a decorator node as a known tool decorator. Returns framework name or None."""
    # @function_tool or @tool (standalone)
    if isinstance(dec, ast.Name):
        return framework_imports.get(dec.id)
    # @function_tool(...) or @tool(...)
    if isinstance(dec, ast.Call) and isinstance(dec.func, ast.Name):
        return framework_imports.get(dec.func.id)
    # @agent.tool or @agent.tool_plain
    if isinstance(dec, ast.Attribute):
        if isinstance(dec.value, ast.Name) and dec.value.id in agent_vars:
            if dec.attr in PYDANTIC_AI_TOOL_ATTRS:
                return "pydantic-ai"
    # @agent.tool(...) or @agent.tool_plain(...)
    if isinstance(dec, ast.Call) and isinstance(dec.func, ast.Attribute):
        if isinstance(dec.func.value, ast.Name) and dec.func.value.id in agent_vars:
            if dec.func.attr in PYDANTIC_AI_TOOL_ATTRS:
                return "pydantic-ai"
    return None


def extract_tool_name(dec, func_name, framework):
    """Extract explicit tool name from decorator args, or fall back to func_name."""
    if not isinstance(dec, ast.Call):
        return func_name
    for kw in dec.keywords:
        if framework == "openai-agents" and kw.arg == "name_override":
            if isinstance(kw.value, ast.Constant) and isinstance(kw.value.value, str):
                return kw.value.value
        if kw.arg == "name":
            if isinstance(kw.value, ast.Constant) and isinstance(kw.value.value, str):
                return kw.value.value
    # Check first positional arg (gauntlet-style)
    if dec.args and isinstance(dec.args[0], ast.Constant) and isinstance(dec.args[0].value, str):
        return dec.args[0].value
    return func_name


def has_run_context(node):
    """Check if the first parameter has a RunContext type annotation."""
    args = node.args
    all_args = args.posonlyargs + args.args
    for arg in all_args:
        if arg.arg in ("self", "cls"):
            continue
        if arg.annotation:
            ann_str = ast.unparse(arg.annotation)
            if "RunContext" in ann_str:
                return True
        break
    return False


def extract_params(node, framework):
    """Extract function parameters, skipping self/cls and RunContext."""
    params = []
    args = node.args
    all_args = args.posonlyargs + args.args
    defaults_offset = len(all_args) - len(args.defaults)

    for i, arg in enumerate(all_args):
        if arg.arg in ("self", "cls"):
            continue
        # Skip RunContext params for pydantic-ai
        if framework == "pydantic-ai" and arg.annotation:
            ann_str = ast.unparse(arg.annotation)
            if "RunContext" in ann_str:
                continue

        param = {"name": arg.arg}
        if arg.annotation:
            param["type"] = ast.unparse(arg.annotation)
        default_idx = i - defaults_offset
        if default_idx >= 0 and default_idx < len(args.defaults):
            try:
                param["default"] = ast.unparse(args.defaults[default_idx])
            except Exception:
                pass
        params.append(param)

    # Keyword-only args
    kw_defaults_map = dict(zip(args.kwonlyargs, args.kw_defaults))
    for kwarg in args.kwonlyargs:
        param = {"name": kwarg.arg}
        if kwarg.annotation:
            param["type"] = ast.unparse(kwarg.annotation)
        default = kw_defaults_map.get(kwarg)
        if default is not None:
            try:
                param["default"] = ast.unparse(default)
            except Exception:
                pass
        params.append(param)

    return params


def extract_tools_from_file(filepath, target_tools=None):
    """Parse a Python file and extract tool function signatures."""
    try:
        with open(filepath, "r") as f:
            source = f.read()
        tree = ast.parse(source, filename=filepath)
    except (SyntaxError, OSError) as e:
        return []

    # Build import context
    agent_vars = set()
    framework_imports = {}  # local_name -> framework

    for node in ast.walk(tree):
        # Track Agent() assignments
        if isinstance(node, ast.Assign):
            if isinstance(node.value, ast.Call):
                func = node.value.func
                func_name = None
                if isinstance(func, ast.Name):
                    func_name = func.id
                elif isinstance(func, ast.Attribute):
                    func_name = func.attr
                if func_name == "Agent":
                    for target in node.targets:
                        if isinstance(target, ast.Name):
                            agent_vars.add(target.id)

        # Track framework imports
        if isinstance(node, ast.ImportFrom) and node.module:
            module = node.module
            for alias in (node.names or []):
                local = alias.asname or alias.name
                # OpenAI Agents SDK
                if module in ("agents", "agents.tool") and alias.name == "function_tool":
                    framework_imports[local] = "openai-agents"
                # LangChain
                if module in ("langchain_core.tools", "langchain.tools") and alias.name == "tool":
                    framework_imports[local] = "langchain"
                # Gauntlet
                if module == "gauntlet" and alias.name == "tool":
                    framework_imports[local] = "gauntlet"

    # Extract tool functions
    tools = []
    for node in ast.walk(tree):
        if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            continue
        for dec in node.decorator_list:
            framework = classify_decorator(dec, agent_vars, framework_imports)
            if framework is None:
                continue
            tool_name = extract_tool_name(dec, node.name, framework)
            if target_tools and tool_name not in target_tools and node.name not in target_tools:
                continue
            return_type = ast.unparse(node.returns) if node.returns else None
            tools.append({
                "name": tool_name,
                "func_name": node.name,
                "file": filepath,
                "line": node.lineno,
                "is_async": isinstance(node, ast.AsyncFunctionDef),
                "params": extract_params(node, framework),
                "return_type": return_type,
                "has_run_context": has_run_context(node),
                "framework": framework,
                "docstring": ast.get_docstring(node),
            })
            break

    return tools


def main():
    input_data = json.load(sys.stdin)
    all_tools = []
    for file_spec in input_data.get("files", []):
        filepath = file_spec["path"]
        target_tools = file_spec.get("tools")
        extracted = extract_tools_from_file(filepath, target_tools)
        all_tools.extend(extracted)
    json.dump({"tools": all_tools}, sys.stdout, indent=2)


if __name__ == "__main__":
    main()
