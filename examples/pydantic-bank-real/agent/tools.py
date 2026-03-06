"""Bank Support tools — wraps real PydanticAI types with @gauntlet.tool.

Unlike the synthetic pydantic-bank example, this file imports REAL classes
from the pydantic-ai package (Agent, RunContext, BaseModel) to prove that
Gauntlet's @gauntlet.tool decorator works with actual PydanticAI framework
types — not just standalone functions.

The tool functions mirror pydantic-ai's bank_support example:
https://github.com/pydantic/pydantic-ai/blob/main/examples/pydantic_ai_examples/bank_support.py
"""

from dataclasses import dataclass

import gauntlet

# --- Real PydanticAI imports (proves framework compatibility) ---
from pydantic import BaseModel
from pydantic_ai import Agent, RunContext


# --- Real PydanticAI structured output model ---
class SupportOutput(BaseModel):
    """Structured output matching pydantic-ai's bank_support SupportOutput."""
    support_advice: str
    block_card: bool
    risk: int


# --- Real PydanticAI dependency injection types ---
@dataclass
class SupportDependencies:
    """Dependencies for the bank support agent (matches pydantic-ai pattern)."""
    customer_id: int
    db_path: str = ":memory:"


# --- Real PydanticAI Agent (dormant in Phase 1, used in Phase 2) ---
support_agent = Agent(
    "test",  # model name — irrelevant in recorded mode (proxy serves fixtures)
    deps_type=SupportDependencies,
    output_type=SupportOutput,
    system_prompt="You are a bank support agent. Help customers with their banking needs.",
)


# --- Tools wrapped with @gauntlet.tool for fixture interception ---

@gauntlet.tool(name="customer_balance")
def customer_balance(customer_id: int) -> str:
    """Returns the customer's current account balance.

    The real pydantic-ai bank_support uses:
        @support_agent.tool
        async def customer_balance(ctx: RunContext[SupportDependencies]) -> str

    Under Gauntlet recorded mode, the fixture is returned WITHOUT calling
    this function body. In live/passthrough mode, it queries SQLite.
    """
    import os
    import sqlite3

    db_path = os.environ.get("GAUNTLET_DB_BANK_DB", ":memory:")
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    res = cur.execute("SELECT balance FROM customers WHERE id=?", (customer_id,))
    row = res.fetchone()
    conn.close()
    if row:
        return f"${row[0]:.2f}"
    raise ValueError(f"Customer {customer_id} not found")


@gauntlet.tool(name="customer_name")
def customer_name(customer_id: int) -> str:
    """Returns the customer's name.

    In the real pydantic-ai example, this would be another tool registered
    on the Agent. Under Gauntlet, returns fixture data.
    """
    import os
    import sqlite3

    db_path = os.environ.get("GAUNTLET_DB_BANK_DB", ":memory:")
    conn = sqlite3.connect(db_path)
    cur = conn.cursor()
    res = cur.execute("SELECT name FROM customers WHERE id=?", (customer_id,))
    row = res.fetchone()
    conn.close()
    if row:
        return row[0]
    return "Unknown"
