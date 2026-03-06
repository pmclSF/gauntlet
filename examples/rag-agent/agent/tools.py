import time
import gauntlet


@gauntlet.tool(name="retrieve_documents")
def retrieve_documents(query: str, top_k: int = 2) -> dict:
    q = query.lower()
    if "timeout" in q:
        time.sleep(0.05)
    if "empty" in q:
        return {"documents": []}

    corpus = [
        {"id": "doc-101", "title": "Shipping SLA", "snippet": "Orders ship within 2 business days."},
        {"id": "doc-205", "title": "Returns Policy", "snippet": "Returns accepted within 30 days."},
        {"id": "doc-309", "title": "Payment Retry", "snippet": "Payment retries occur every 24 hours."},
    ]
    return {"documents": corpus[:max(1, top_k)]}
