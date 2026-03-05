# Contributing to Gauntlet

Thanks for your interest in contributing to Gauntlet.

## Getting started

```bash
git clone https://github.com/pmclSF/gauntlet.git
cd gauntlet
make build
make test
```

## What to work on

The most useful contributions right now:

- **New tool state patterns** — open an issue describing the pattern
- **Framework adapters** — add to `sdk/python/gauntlet/adapters/`
- **Additional assertion types** — implement in `internal/assertions/`
- **Bug reports** — open an issue with reproduction steps

## Code structure

- `cmd/gauntlet/` — CLI commands
- `internal/` — all Go implementation
- `sdk/python/gauntlet/` — Python SDK
- `examples/` — example agents with scenarios
- `schema/` — JSON schemas

## Development workflow

1. Create a branch from `main`
2. Make your changes
3. Run `make test` and `make lint`
4. Open a pull request

## Testing

```bash
make test           # all Go tests with -race
make test-example   # integration test against examples/support-agent
make lint           # golangci-lint
```

## Style

- Go code follows standard `gofmt` formatting
- Every user-facing error includes: what failed, expected vs actual, file path, and fix command
- Assertions are either hard gates (block CI) or soft signals (report only) — never ambiguous
