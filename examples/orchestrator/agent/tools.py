import gauntlet


@gauntlet.tool(name="plan_task")
def plan_task(task: str) -> dict:
    return {"plan_id": "plan-001", "task": task}


@gauntlet.tool(name="fetch_context")
def fetch_context(plan_id: str, attempt: int) -> dict:
    if attempt == 1:
        return {"error": "transient"}
    return {"context": f"context-for-{plan_id}"}


@gauntlet.tool(name="finalize_response")
def finalize_response(plan_id: str, context: str) -> dict:
    return {"response": f"Plan {plan_id} complete with {context}"}
