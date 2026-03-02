"""Support Agent tools — decorated with @gauntlet.tool for fixture replay."""

import gauntlet


@gauntlet.tool(name="order_lookup")
def order_lookup(order_id: str) -> dict:
    """Look up an order by ID.

    In production, this calls the real orders API.
    Under Gauntlet, returns fixture data without calling the API.
    """
    import requests
    resp = requests.get(f"https://api.example.com/orders/{order_id}")
    return resp.json()


@gauntlet.tool(name="web_search")
def web_search(query: str) -> dict:
    """Search the web for information.

    In production, this calls a search API.
    Under Gauntlet, returns fixture data.
    """
    import requests
    resp = requests.get(
        "https://api.example.com/search",
        params={"q": query},
    )
    return resp.json()


@gauntlet.tool(name="send_email")
def send_email(to: str, subject: str, body: str) -> dict:
    """Send an email to a customer.

    In production, this calls an email API.
    Under Gauntlet, returns fixture data.
    """
    import requests
    resp = requests.post(
        "https://api.example.com/email/send",
        json={"to": to, "subject": subject, "body": body},
    )
    return resp.json()
