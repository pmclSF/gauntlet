"""Provider adapters for Gauntlet SDK."""

from gauntlet.adapters.anthropic import (
    install_anthropic_instrumentation,
    patch_anthropic_client,
)
from gauntlet.adapters.langchain import (
    get_gauntlet_callbacks,
    install_langchain_instrumentation,
    patch_langchain_llm,
)
from gauntlet.adapters.openai import (
    install_openai_instrumentation,
    patch_openai_client,
)

__all__ = [
    "install_anthropic_instrumentation",
    "install_langchain_instrumentation",
    "install_openai_instrumentation",
    "patch_anthropic_client",
    "patch_openai_client",
    "patch_langchain_llm",
    "get_gauntlet_callbacks",
]
