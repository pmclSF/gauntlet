"""Weather Agent — Real OpenAI Agents SDK Gauntlet TUT.

Unlike the synthetic openai-weather example, this imports REAL OpenAI Agents
SDK classes (Agent, Runner, function_tool) from the installed openai-agents
package. This proves that Gauntlet works with actual third-party agent
framework code.

Phase 1 (current): Rule-based handler that uses @gauntlet.tool-wrapped
tools for deterministic fixture replay. No LLM/API calls needed.

Phase 2 (future): Replace rule-based handler with actual Runner.run()
call, where model API calls are intercepted by the MITM proxy.
"""

import json
import os
import sys

import gauntlet

gauntlet.connect()

from agent.tools import Weather, get_weather, weather_agent


def handle_request(input_data: dict) -> dict:
    """Process a weather request using real OpenAI Agents SDK types.

    Produces output with response and tool_calls fields.
    """
    messages = input_data.get("messages", [])
    if not messages:
        return {"response": "No input provided.", "tool_calls": []}

    last_message = messages[-1].get("content", "")
    lower = last_message.lower()
    tool_calls = []

    # --- Weather query: extract city and call tool ---
    if "weather" in lower:
        # Extract city from common patterns
        city = None
        for keyword in ["in ", "for ", "at "]:
            if keyword in lower:
                city = last_message.split(keyword)[-1].strip().rstrip("?!.")
                break

        if city:
            result = get_weather(city=city)
            tool_calls.append({
                "tool": "get_weather",
                "args": {"city": city},
                "result": result,
            })
            # Validate result matches Weather model shape
            weather = Weather(**result) if isinstance(result, dict) else result
            return {
                "response": f"The weather in {weather.city} is {weather.conditions} "
                           f"with temperatures of {weather.temperature_range}.",
                "tool_calls": tool_calls,
            }
        else:
            # No city specified — agent should ask for clarification
            return {
                "response": "Which city would you like the weather for?",
                "tool_calls": [],
            }

    # --- Non-weather request: agent should NOT call get_weather ---
    # Intentional bug: the agent calls get_weather anyway for some requests
    if "email" in lower or "send" in lower:
        # Buggy behavior: calls get_weather for non-weather requests
        result = get_weather(city="Unknown")
        tool_calls.append({
            "tool": "get_weather",
            "args": {"city": "Unknown"},
            "result": result,
        })
        return {
            "response": "I can only help with weather queries.",
            "tool_calls": tool_calls,
        }

    return {
        "response": "I'm a weather agent. Ask me about the weather!",
        "tool_calls": [],
    }


def main():
    """Run as CLI agent reading JSON-lines from stdin."""
    os.environ.setdefault("PYTHONUNBUFFERED", "1")

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            input_data = json.loads(line)
            result = handle_request(input_data)
            print(json.dumps(result))
            sys.stdout.flush()
        except Exception as e:
            error_resp = {"error": str(e), "tool_calls": []}
            print(json.dumps(error_resp))
            sys.stdout.flush()


if __name__ == "__main__":
    main()
