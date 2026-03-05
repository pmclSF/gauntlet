# Releasing Gauntlet

This document describes the release flow for both the Go CLI and the Python SDK.

## Prerequisites

- Clean main branch with all tests passing.
- Access to push Git tags.
- PyPI credentials (or trusted publisher) for `gauntlet-sdk`.
- GitHub CLI authenticated (`gh auth status`).

## 1. Bump versions

Update versions in:

- `cmd/gauntlet/main.go` (`var version = "..."`)
- `sdk/python/setup.py` (`version="..."`)
- Any pinned SDK/CLI references in CI templates if a coordinated pin bump is desired.

## 2. Validate locally

Run:

```bash
go test ./...
PYTHONPATH=sdk/python python3 -m pytest -q sdk/python/tests
```

Optionally build binaries:

```bash
go build ./cmd/gauntlet
```

## 3. Tag and publish GitHub release

```bash
git add -A
git commit -m "release: vX.Y.Z"
git tag vX.Y.Z
git push origin main --follow-tags
gh release create vX.Y.Z --generate-notes
```

## 4. Publish Python SDK to PyPI

From `sdk/python`:

```bash
python3 -m pip install --upgrade build twine
python3 -m build
python3 -m twine upload dist/*
```

Verify:

```bash
python3 -m pip index versions gauntlet-sdk
```

## 5. Install script and docs check

Ensure install instructions reference this repo:

- `README.md` quickstart install command:
  - `go install github.com/pmclSF/gauntlet/cmd/gauntlet@latest`
- `install.sh`:
  - `go install github.com/pmclSF/gauntlet/cmd/gauntlet@latest`

If release policy changes pinning strategy, update both files in the same release PR.

## 6. Post-release verification

- Fresh shell:
  - `go install github.com/pmclSF/gauntlet/cmd/gauntlet@latest`
  - `gauntlet --version`
- Fresh virtualenv:
  - `pip install gauntlet-sdk==X.Y.Z`
  - `python -c "import gauntlet; print('ok')"`
