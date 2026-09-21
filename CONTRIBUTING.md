# Contributing to OpenLia

OpenLia is an early-stage operations and setup layer for Hermes Agent. Keep
changes small, reviewable, and explicit about their operational or security
impact.

## Development Requirements

- Go 1.26 or newer
- Python 3.9 or newer (with virtual environment setup via `make venv`)
- Docker Compose v2 for Compose validation
- Bash and ShellCheck for the small container and cron adapters

Live credentials, personal workspace data, runtime state, Locho capabilities,
and private deployment identifiers must remain outside the repository.

## Local Checks

Run the relevant checks from the repository root:

```sh
make venv            # Sets up .venv/ and installs all dependencies (uses uv if present)
make test            # Runs Go, Python, and skill self-tests
make skills-test     # Offline skill self-test suite across all 13 skills
make skills-verify   # Live browser verifications (with Playwright MCP running locally)
make lint
make compose-config
python3 tests/smoke.py --mode cli
python3 tests/smoke.py --mode local --root /tmp/openlia_smoke
make smoke-local-live \
  OPENLIA_SMOKE_ENV_FILE="$HOME/.config/openlia/dev/openlia_dev.env" \
  OPENLIA_SMOKE_ATTACHMENTS_FILE="$HOME/.config/openlia/dev/locho-attachments.toml" \
  OPENLIA_SMOKE_LOCHO_HOST=genai
```

For developing and verifying skills locally without deploying them to a running agent, see [`docs/SKILL_DEVELOPMENT.md`](docs/SKILL_DEVELOPMENT.md).

The CLI smoke mode uses synthetic data and must not require provider
credentials, Telegram, Locho, SSH, or a remote target. The local deployment
smoke additionally requires a working Linux Docker engine, provided directly by
Docker Engine on Linux or Docker Desktop on macOS.

`make test` runs the Go operator and filesystem integration tests without a
provider. Run `make smoke-local` separately for an actual local Compose
deployment check.

The credential-backed local target is intentionally not part of the default
test suite. It starts a real agent with external credentials and leaves the
installation running unless `--cleanup` is supplied. Do not put those paths or
values in repository files.

## Changes

- Preserve the approval boundaries and secret-handling rules documented in the
  repository.
- Do not add credentials, personal records, generated runtime state, or private
  infrastructure details to source files, tests, examples, or documentation.
- Add or update tests for behavior changes.
- Keep pinned runtime versions and checksums synchronized with release metadata.
- Document operational assumptions and known limitations.

Before submitting a change, review the complete staged file list and confirm
that no local or generated files are included.
