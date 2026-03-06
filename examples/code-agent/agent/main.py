import json
import sys

import gauntlet
from agent.tools import sandbox_exec


gauntlet.connect()


def handle_request(payload: dict) -> dict:
    messages = payload.get("messages", [])
    request = str(messages[-1].get("content", "")) if messages else ""
    generated_code = "def add(a, b):\n    return a + b\n"
    execution = sandbox_exec(code=generated_code)
    return {
        "response": f"Generated helper for request: {request}",
        "generated_code": generated_code,
        "sandbox": execution,
    }


def main() -> None:
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            print(json.dumps(handle_request(req)))
        except Exception as exc:  # noqa: BLE001
            print(json.dumps({"error": str(exc)}))
        sys.stdout.flush()


if __name__ == "__main__":
    main()
