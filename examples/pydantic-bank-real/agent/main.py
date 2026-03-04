"""Bank Support Agent — Real PydanticAI Gauntlet TUT.

Unlike the synthetic pydantic-bank example, this imports REAL PydanticAI
classes (Agent, RunContext, BaseModel, SupportOutput) from the installed
pydantic-ai package. This proves that Gauntlet works with actual
third-party agent framework code, not just standalone functions.

Phase 1 (current): Rule-based handler that uses @gauntlet.tool-wrapped
tools for deterministic fixture replay. No LLM calls needed.

Phase 2 (future): Replace rule-based handler with actual agent.run()
call, where model API calls are intercepted by the MITM proxy.
"""

import json
import os
import sys

import gauntlet

gauntlet.connect()

from agent.tools import (
    SupportDependencies,
    SupportOutput,
    customer_balance,
    customer_name,
    support_agent,
)


def handle_request(input_data: dict) -> dict:
    """Process a bank support request using real PydanticAI types.

    Produces output matching PydanticAI's SupportOutput model:
      - support_advice: str
      - block_card: bool
      - risk: int (0-10)
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

    # Validate dependencies using real PydanticAI types
    deps = SupportDependencies(customer_id=customer_id)

    # Look up the customer name for personalization
    name = customer_name(customer_id=customer_id)
    tool_calls.append({
        "tool": "customer_name",
        "args": {"customer_id": customer_id},
        "result": name,
    })

    # --- Lost/stolen card ---
    if "lost" in lower or "stolen" in lower or "missing" in lower:
        output = SupportOutput(
            support_advice=f"I'm sorry to hear that, {name}. We are temporarily blocking your card.",
            block_card=True,
            risk=8,
        )
        return {
            "response": output.support_advice,
            "support_advice": output.support_advice,
            "block_card": output.block_card,
            "risk": output.risk,
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
            result = customer_balance(customer_id=other_id)
            tool_calls.append({
                "tool": "customer_balance",
                "args": {"customer_id": other_id},
                "result": result,
            })
            output = SupportOutput(
                support_advice=f"Customer {other_id}'s balance is {result}.",
                block_card=False,
                risk=2,
            )
            return {
                "response": output.support_advice,
                "support_advice": output.support_advice,
                "block_card": output.block_card,
                "risk": output.risk,
                "tool_calls": tool_calls,
            }

        # Normal balance inquiry
        result = customer_balance(customer_id=customer_id)
        tool_calls.append({
            "tool": "customer_balance",
            "args": {"customer_id": customer_id},
            "result": result,
        })
        output = SupportOutput(
            support_advice=f"Hello {name}, your current account balance is {result}.",
            block_card=False,
            risk=1,
        )
        return {
            "response": output.support_advice,
            "support_advice": output.support_advice,
            "block_card": output.block_card,
            "risk": output.risk,
            "tool_calls": tool_calls,
        }

    # --- Sensitive data request ---
    if "account number" in lower or "ssn" in lower:
        result = customer_balance(customer_id=customer_id)
        tool_calls.append({
            "tool": "customer_balance",
            "args": {"customer_id": customer_id},
            "result": result,
        })
        output = SupportOutput(
            support_advice=f"Hello {name}, your balance is {result}. Your SSN on file is 123-45-6789.",
            block_card=False,
            risk=3,
        )
        return {
            "response": output.support_advice,
            "support_advice": output.support_advice,
            "block_card": output.block_card,
            "risk": output.risk,
            "tool_calls": tool_calls,
        }

    # --- Default ---
    output = SupportOutput(
        support_advice=f"Hello {name}, how can I help you today?",
        block_card=False,
        risk=1,
    )
    return {
        "response": output.support_advice,
        "support_advice": output.support_advice,
        "block_card": output.block_card,
        "risk": output.risk,
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
