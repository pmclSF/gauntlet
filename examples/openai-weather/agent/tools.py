"""Weather Agent tools — decorated with @gauntlet.tool for fixture replay.

Modeled after the OpenAI Agents SDK basic/tools.py example:
https://github.com/openai/openai-agents-python/blob/main/examples/basic/tools.py
"""

import gauntlet


@gauntlet.tool(name="get_weather")
def get_weather(city: str) -> dict:
    """Get the current weather information for a specified city.

    In production, this calls a real weather API.
    Under Gauntlet, returns fixture data without making any network calls.
    """
    import requests
    resp = requests.get(f"https://api.weather.example.com/v1/current?city={city}")
    return resp.json()
