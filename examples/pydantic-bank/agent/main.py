"""Bank Support Agent — PydanticAI-style Gauntlet TUT.

Modeled after pydantic-ai's bank_support example. Implements a
rule-based handler that mirrors how a PydanticAI agent with
SupportOutput(support_advice, block_card, risk) would behave.

Uses @gauntlet.tool decorated tools for deterministic fixture replay.
"""

import json
import os
import sys

import gauntlet

gauntlet.connect()

from agent.tools import customer_balance, customer_name


def handle_request(input_data: dict) -> dict:
    """Process a bank support request.

    Produces structured output matching PydanticAI's SupportOutput:
      - support_advice: str
      - block_card: bool
      - risk: int (1-10)
      - tool_calls: list of tool invocations
    """
    messages = input_data.get("messages", [])
    if not messages:
        return {
            "response": "No input provided.",
            "support_advice": "No input provided.",
            "block_card": False,
            "risk": 0,
            "tool_calls": [],
        }

    last_message = messages[-1].get("content", "")
    lower = last_message.lower()
    tool_calls = []

    # Extract customer ID from context (default: 123)
    customer_id = input_data.get("customer_id", 123)

    # Look up the customer name for personalization
    name = customer_name(customer_id=customer_id)
    tool_calls.append({
        "tool": "customer_name",
        "args": {"customer_id": customer_id},
        "result": name,
    })

    # --- Lost/stolen card ---
    if "lost" in lower or "stolen" in lower or "missing" in lower:
        return {
            "response": f"I'm sorry to hear that, {name}. We are temporarily blocking your card to prevent unauthorized transactions.",
            "support_advice": f"I'm sorry to hear that, {name}. We are temporarily blocking your card to prevent unauthorized transactions.",
            "block_card": True,
            "risk": 8,
            "tool_calls": tool_calls,
        }

    # --- Balance inquiry ---
    if "balance" in lower or "how much" in lower:
        # Check if asking about ANOTHER customer (forbidden scenario)
        other_id = None
        for word in last_message.split():
            if word.isdigit() and int(word) != customer_id:
                other_id = int(word)
                break
        if "customer" in lower and other_id is not None:
            # Agent should NOT look up another customer's balance
            # But a buggy agent does it anyway
            result = customer_balance(customer_id=other_id)
            tool_calls.append({
                "tool": "customer_balance",
                "args": {"customer_id": other_id},
                "result": result,
            })
            return {
                "response": f"Customer {other_id}'s balance is {result}.",
                "support_advice": f"Customer {other_id}'s balance is {result}.",
                "block_card": False,
                "risk": 2,
                "tool_calls": tool_calls,
            }

        # Normal balance inquiry
        result = customer_balance(customer_id=customer_id)
        tool_calls.append({
            "tool": "customer_balance",
            "args": {"customer_id": customer_id},
            "result": result,
        })
        return {
            "response": f"Hello {name}, your current account balance is {result}.",
            "support_advice": f"Hello {name}, your current account balance is {result}.",
            "block_card": False,
            "risk": 1,
            "tool_calls": tool_calls,
        }

    # --- Account number / sensitive data request ---
    if "account number" in lower or "ssn" in lower:
        result = customer_balance(customer_id=customer_id)
        tool_calls.append({
            "tool": "customer_balance",
            "args": {"customer_id": customer_id},
            "result": result,
        })
        # Buggy agent: leaks the SSN from the database
        return {
            "response": f"Hello {name}, your balance is {result}. Your SSN on file is 123-45-6789.",
            "support_advice": f"Hello {name}, your balance is {result}. Your SSN on file is 123-45-6789.",
            "block_card": False,
            "risk": 3,
            "tool_calls": tool_calls,
        }

    # --- Default ---
    return {
        "response": f"Hello {name}, how can I help you today?",
        "support_advice": f"Hello {name}, how can I help you today?",
        "block_card": False,
        "risk": 1,
        "tool_calls": tool_calls,
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
