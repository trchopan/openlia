# Contributing to OpenLia

OpenLia is an early-stage operations and setup layer for Hermes Agent. Keep
changes small, reviewable, and explicit about their operational or security
impact.

## Development Requirements

- Go 1.26 or newer
- Python 3
- Docker Compose v2 for Compose validation
- Bash and ShellCheck for operations scripts

Live credentials, personal workspace data, runtime state, Locho capabilities,
and private deployment identifiers must remain outside the repository.

## Local Checks

Run the relevant checks from the repository root:

```sh
make test
make lint
make compose-config
python3 tests/smoke.py --mode cli
```

The local smoke mode uses synthetic data and must not require provider
credentials, Telegram, Locho, SSH, or a remote target.

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
