import json
import sys

import gauntlet
from agent.tools import fetch_context, finalize_response, plan_task


gauntlet.connect()


def handle_request(payload: dict) -> dict:
    messages = payload.get("messages", [])
    request = str(messages[-1].get("content", "")) if messages else ""

    plan = plan_task(task=request)
    plan_id = plan["plan_id"]

    first = fetch_context(plan_id=plan_id, attempt=1)
    attempts = 1
    if "error" in first:
        second = fetch_context(plan_id=plan_id, attempt=2)
        attempts = 2
        context = second.get("context", "context-missing")
    else:
        context = first.get("context", "context-missing")

    finalized = finalize_response(plan_id=plan_id, context=context)
    return {
        "response": finalized.get("response", ""),
        "plan_id": plan_id,
        "attempts": attempts,
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
