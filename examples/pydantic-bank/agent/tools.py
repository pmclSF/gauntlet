"""Bank Support Agent tools — decorated with @gauntlet.tool for fixture replay.

Modeled after the pydantic-ai bank_support example:
https://github.com/pydantic/pydantic-ai/blob/main/examples/pydantic_ai_examples/bank_support.py
"""

import gauntlet


@gauntlet.tool(name="customer_balance")
def customer_balance(customer_id: int) -> str:
    """Returns the customer's current account balance.

    In production, this queries a real database.
    Under Gauntlet, returns fixture data without hitting the DB.
    """
    import sqlite3
    import os

    db_url = os.environ.get("GAUNTLET_DB_BANK_DB", ":memory:")
    conn = sqlite3.connect(db_url)
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

    In production, this queries a real database.
    Under Gauntlet, returns fixture data.
    """
    import sqlite3
    import os

    db_url = os.environ.get("GAUNTLET_DB_BANK_DB", ":memory:")
    conn = sqlite3.connect(db_url)
    cur = conn.cursor()
    res = cur.execute("SELECT name FROM customers WHERE id=?", (customer_id,))
    row = res.fetchone()
    conn.close()
    if row:
        return row[0]
    return "Unknown"
