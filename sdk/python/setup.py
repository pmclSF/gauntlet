from setuptools import setup, find_packages

with open("README.md", "r", encoding="utf-8") as f:
    long_description = f.read()

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
        "dev": [
            "pytest>=7.0",
            "mypy>=1.8",
            "pyright>=1.1.390",
        ],
    },
    description="Gauntlet SDK — deterministic scenario testing for agentic systems",
    long_description=long_description,
    long_description_content_type="text/markdown",
    url="https://github.com/pmclSF/gauntlet",
    license="MIT",
    classifiers=[
        "Development Status :: 3 - Alpha",
        "Intended Audience :: Developers",
        "License :: OSI Approved :: MIT License",
        "Programming Language :: Python :: 3",
        "Programming Language :: Python :: 3.9",
        "Programming Language :: Python :: 3.10",
        "Programming Language :: Python :: 3.11",
        "Programming Language :: Python :: 3.12",
        "Topic :: Software Development :: Testing",
    ],
)
