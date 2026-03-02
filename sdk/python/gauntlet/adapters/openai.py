"""OpenAI SDK adapter for Gauntlet.

When Gauntlet is enabled, the HTTPS_PROXY env var routes all OpenAI API calls
through the Gauntlet proxy. This adapter provides additional utilities for
inspecting and tracing OpenAI-specific calls.
"""

import os
from typing import Optional


def patch_openai_client(client=None):
    """Patch an OpenAI client to route through Gauntlet proxy.

    In most cases, setting HTTPS_PROXY is sufficient and this function
    is not needed. Use this for clients that explicitly set a base_url
    or disable proxy support.
    """
    if os.environ.get("GAUNTLET_ENABLED") != "1":
        return client

    proxy_addr = os.environ.get("HTTPS_PROXY") or os.environ.get("https_proxy")
    if not proxy_addr:
        return client

    if client is None:
        try:
            import openai

            client = openai.OpenAI()
        except ImportError:
            raise ImportError("openai package required: pip install openai")

    return client


def get_model_from_env() -> Optional[str]:
    """Get the model override from Gauntlet env, if set."""
    return os.environ.get("GAUNTLET_MODEL_OVERRIDE")
