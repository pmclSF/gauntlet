"""Support Agent — Example Gauntlet TUT (Target Under Test).

A FastAPI HTTP agent that handles customer support queries.
Uses @gauntlet.tool decorated tools for deterministic testing.
"""

import json
import os
import sys

import gauntlet

# Initialize Gauntlet SDK (no-op in production)
gauntlet.connect()

from agent.tools import order_lookup, web_search, send_email
from agent.prompts import SYSTEM_PROMPT


def handle_request(input_data: dict) -> dict:
    """Process a customer support request.

    In a real agent, this would call an LLM. For the example,
    we implement a simple rule-based handler that demonstrates
    tool usage patterns.
    """
    messages = input_data.get("messages", [])
    if not messages:
        return {"response": "No input provided", "tool_calls": []}

    last_message = messages[-1].get("content", "")
    tool_calls = []

    # Simple routing logic
    if "order" in last_message.lower() or "status" in last_message.lower():
        # Extract order ID
        order_id = None
        for word in last_message.split():
            normalized = word.strip(".,!?\"'()[]{}")
            if normalized.startswith("ord-") or normalized.startswith("ORD-"):
                order_id = normalized
                break

        if order_id:
            result = order_lookup(order_id=order_id)
            tool_calls.append({"tool": "order_lookup", "args": {"order_id": order_id}, "result": result})

            if result.get("payment_status") == "conflicting":
                # Should look up payment details but doesn't in this buggy path
                return {
                    "response": f"Order {order_id} has status: {result.get('status', 'unknown')}. Payment status appears conflicting.",
                    "tool_calls": tool_calls,
                }

            return {
                "response": f"Order {order_id} status: {result.get('status', 'unknown')}",
                "tool_calls": tool_calls,
            }

    if "search" in last_message.lower() or "find" in last_message.lower():
        query = last_message
        result = web_search(query=query)
        tool_calls.append({"tool": "web_search", "args": {"query": query}, "result": result})

        # Potentially unsafe: echoing search results directly
        snippets = [r.get("snippet", "") for r in result.get("results", [])]
        return {
            "response": f"Search results: {'; '.join(snippets)}",
            "tool_calls": tool_calls,
        }

    if "email" in last_message.lower() or "send" in last_message.lower():
        # This should be gated — some scenarios forbid email
        result = send_email(
            to="customer@example.com",
            subject="Support Response",
            body="Thank you for contacting support.",
        )
        tool_calls.append({
            "tool": "send_email",
            "args": {"to": "customer@example.com", "subject": "Support Response"},
            "result": result,
        })
        return {
            "response": "Email sent to customer.",
            "tool_calls": tool_calls,
        }

    return {
        "response": "I can help with order lookups, searches, and sending emails. What would you like to do?",
        "tool_calls": tool_calls,
    }


def main():
    """Run as CLI agent reading JSON from stdin."""
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
