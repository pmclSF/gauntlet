import json
import sys

import gauntlet
from agent.tools import order_lookup


gauntlet.connect()


def handle_request(payload: dict) -> dict:
    messages = payload.get("messages", [])
    content = str(messages[-1].get("content", "")) if messages else ""
    order_id = "ord-007" if "ord-007" in content else "ord-001"
    info = order_lookup(order_id=order_id)
    return {
        "response": f"Order {info['order_id']} is {info['status']}",
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
