"""Weather Agent — OpenAI Agents SDK-style Gauntlet TUT.

Modeled after openai-agents-python's basic/tools.py example.
Implements a rule-based handler that mirrors how an OpenAI Agent
with a get_weather tool would behave.

Uses @gauntlet.tool decorated tools for deterministic fixture replay.
"""

import json
import os
import sys

import gauntlet

gauntlet.connect()

from agent.tools import get_weather


def handle_request(input_data: dict) -> dict:
    """Process a weather query request.

    Produces output matching what an OpenAI Agent with get_weather would return:
      - response: str (natural language weather summary)
      - tool_calls: list of tool invocations
    """
    messages = input_data.get("messages", [])
    if not messages:
        return {"response": "No input provided.", "tool_calls": []}

    last_message = messages[-1].get("content", "")
    lower = last_message.lower()
    tool_calls = []

    # --- Weather query ---
    if "weather" in lower:
        # Extract city name (simple heuristic)
        city = extract_city(last_message)

        if city:
            result = get_weather(city=city)
            tool_calls.append({
                "tool": "get_weather",
                "args": {"city": city},
                "result": result,
            })
            # Format weather response
            if isinstance(result, dict):
                temp = result.get("temperature_range", "unknown")
                conditions = result.get("conditions", "unknown")
                return {
                    "response": f"The weather in {city} is {conditions} with temperatures of {temp}.",
                    "tool_calls": tool_calls,
                }
            return {
                "response": f"Weather data for {city}: {result}",
                "tool_calls": tool_calls,
            }
        else:
            return {
                "response": "I'd be happy to help with weather information. Which city would you like to know about?",
                "tool_calls": tool_calls,
            }

    # --- Send email (should be forbidden in some scenarios) ---
    if "email" in lower or "send" in lower:
        # Buggy: calls get_weather even for email requests
        result = get_weather(city="Unknown")
        tool_calls.append({
            "tool": "get_weather",
            "args": {"city": "Unknown"},
            "result": result,
        })
        return {
            "response": "I can only help with weather queries, not sending emails.",
            "tool_calls": tool_calls,
        }

    # --- Default ---
    return {
        "response": "I'm a weather agent. Ask me about the weather in any city!",
        "tool_calls": tool_calls,
    }


def extract_city(text: str) -> str:
    """Extract a city name from a weather query.

    Simple heuristic: look for common patterns like
    'weather in <city>' or 'weather for <city>'.
    """
    lower = text.lower()

    # Pattern: "weather in <city>"
    for preposition in ["in ", "for ", "at "]:
        idx = lower.find(preposition)
        if idx != -1:
            after = text[idx + len(preposition):].strip()
            # Take the first word(s) that look like a city name
            city = after.rstrip("?!.,")
            if city:
                return city

    return ""


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
