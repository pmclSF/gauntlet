import gauntlet


@gauntlet.tool(name="sandbox_exec")
def sandbox_exec(code: str) -> dict:
    return {
        "stdout": "execution-ok",
        "stderr": "",
        "exit_code": 0,
        "code": code,
    }
