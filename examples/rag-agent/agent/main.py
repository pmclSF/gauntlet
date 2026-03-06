import json
import sys

import gauntlet
from agent.tools import retrieve_documents


gauntlet.connect()


def handle_request(payload: dict) -> dict:
    messages = payload.get("messages", [])
    query = ""
    if messages:
        query = str(messages[-1].get("content", ""))

    retrieved = retrieve_documents(query=query, top_k=2)
    docs = retrieved.get("documents", [])
    citations = [doc.get("id") for doc in docs if isinstance(doc, dict) and doc.get("id")]

    if not citations:
        return {
            "response": "No relevant documents found for this question.",
            "citations": [],
        }

    return {
        "response": "Based on retrieved documents, your order processing request is covered.",
        "citations": citations,
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
            print(json.dumps({"error": str(exc), "citations": []}))
        sys.stdout.flush()


if __name__ == "__main__":
    main()
