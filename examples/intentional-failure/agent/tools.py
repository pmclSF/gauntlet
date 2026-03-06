import gauntlet


@gauntlet.tool(name="order_lookup")
def order_lookup(order_id: str) -> dict:
    return {"order_id": order_id, "status": "confirmed"}
