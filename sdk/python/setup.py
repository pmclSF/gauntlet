from setuptools import setup, find_packages

setup(
    name="gauntlet-sdk",
    version="0.1.0",
    packages=find_packages(),
    python_requires=">=3.9",
    install_requires=[],
    extras_require={
        "openai": ["openai>=1.0"],
        "anthropic": ["anthropic>=0.18"],
        "langchain": ["langchain-core>=0.1"],
    },
    description="Gauntlet SDK — deterministic scenario testing for agentic systems",
)
