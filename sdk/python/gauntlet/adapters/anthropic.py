"""Anthropic SDK adapter for Gauntlet.

When Gauntlet is enabled, the HTTPS_PROXY env var routes all Anthropic API calls
through the Gauntlet proxy.
"""

import os
from typing import Optional


def patch_anthropic_client(client=None):
    """Patch an Anthropic client to route through Gauntlet proxy.

    In most cases, setting HTTPS_PROXY is sufficient. Use this for clients
    that explicitly configure their own HTTP transport.
    """
    if os.environ.get("GAUNTLET_ENABLED") != "1":
        return client

    if client is None:
        try:
            import anthropic

            client = anthropic.Anthropic()
        except ImportError:
            raise ImportError("anthropic package required: pip install anthropic")

    return client


def get_model_from_env() -> Optional[str]:
    """Get the model override from Gauntlet env, if set."""
    return os.environ.get("GAUNTLET_MODEL_OVERRIDE")
