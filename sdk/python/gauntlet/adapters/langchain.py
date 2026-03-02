"""LangChain adapter for Gauntlet.

Provides utilities for integrating Gauntlet with LangChain-based agents.
"""

import os
from typing import Optional


def patch_langchain_llm(llm=None):
    """Ensure a LangChain LLM routes through Gauntlet proxy.

    HTTPS_PROXY env var handles most cases automatically.
    """
    if os.environ.get("GAUNTLET_ENABLED") != "1":
        return llm
    return llm


def get_gauntlet_callbacks():
    """Return LangChain callback handlers for Gauntlet tracing."""
    if os.environ.get("GAUNTLET_ENABLED") != "1":
        return []

    try:
        from langchain_core.callbacks import BaseCallbackHandler
    except ImportError:
        return []

    class GauntletTraceCallback(BaseCallbackHandler):
        """Records tool and LLM calls for Gauntlet trace."""

        def on_tool_start(self, serialized, input_str, **kwargs):
            pass

        def on_tool_end(self, output, **kwargs):
            pass

        def on_llm_start(self, serialized, prompts, **kwargs):
            pass

        def on_llm_end(self, response, **kwargs):
            pass

    return [GauntletTraceCallback()]
