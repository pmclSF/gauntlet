"""Weather tools — wraps real OpenAI Agents SDK types with @gauntlet.tool.

Unlike the synthetic openai-weather example, this file imports REAL classes
from the openai-agents package (Agent, Runner, function_tool) to prove that
Gauntlet's @gauntlet.tool decorator works with actual OpenAI Agents SDK
framework types — not just standalone functions.

The tool functions mirror openai-agents-python's basic tools example:
https://github.com/openai/openai-agents-python/blob/main/examples/basic/tools.py
"""

import gauntlet

# --- Real OpenAI Agents SDK imports (proves framework compatibility) ---
from agents import Agent, Runner, function_tool
from pydantic import BaseModel


# --- Real Pydantic model matching OpenAI Agents SDK Weather type ---
class Weather(BaseModel):
    """Weather data model (matches real OpenAI Agents SDK example)."""
    city: str
    temperature_range: str
    conditions: str


# --- Real OpenAI Agents SDK Agent (dormant in Phase 1, used in Phase 2) ---
weather_agent = Agent(
    name="Weather Agent",
    instructions="You are a helpful weather agent. Use the get_weather tool to get weather information.",
)


# --- Tool wrapped with @gauntlet.tool for fixture interception ---

@gauntlet.tool(name="get_weather")
def get_weather(city: str) -> dict:
    """Get the current weather for a city.

    The real OpenAI Agents SDK example uses:
        @function_tool
        def get_weather(city: str) -> Weather

    Under Gauntlet recorded mode, the fixture is returned WITHOUT calling
    this function body. In live/passthrough mode, it returns mock data.
    """
    return Weather(
        city=city,
        temperature_range="14-20C",
        conditions="Sunny with wind.",
    ).model_dump()
