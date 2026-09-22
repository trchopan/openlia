# Skill Development And Testing

OpenLia supports two different skill workflows. Bundled skills are developed in
this repository and shipped with OpenLia. External skills are fetched from
configured GitHub repositories and use the external manager described in
[`EXTERNAL_SKILLS.md`](EXTERNAL_SKILLS.md). Their dependency and test contracts
are intentionally different.

## Bundled Skills

Bundled skills live at `profile/skills/<name>/` and normally contain:

```text
profile/skills/<name>/
|-- SKILL.md
|-- requirements.txt       # Optional; only for dependencies bundled by OpenLia
|-- scripts/               # Optional deterministic helpers
|-- templates/             # Optional Markdown templates
`-- references/            # Optional guidance or rubrics
```

Follow the frontmatter and conventions already used by neighboring bundled
skills. A Python helper should provide a deterministic, offline `--self-test`
when the repository test harness expects one. Run the bundled checks from the
repository root:

```sh
make venv
make skills-test
go run . skills test SKILL_NAME
```

### Browser Skill Verification

Browser-backed bundled skills require a local Playwright MCP server with the
browser extension and shared browser context enabled:

```sh
npx @playwright/mcp@latest --host 127.0.0.1 --port 8931 --extension \
  --idle-timeout 0 --shared-browser-context
```

Run the live canaries only when an authenticated disposable browser session is
available:

```sh
python3 profile/skills/gemini-chat/scripts/gemini_conversation.py --verify
python3 profile/skills/chatgpt-chat/scripts/chatgpt_conversation.py --verify
python3 profile/skills/browser-pilot/scripts/check_browser_pilot.py --verify
make skills-verify
```

Live browser output belongs under `/tmp/openlia_verify/`; it must not write to
the real workspace. The runners prune `.playwright-mcp/` files older than 24
hours and cap retained files. Browser jobs use isolated Temporary Chat flows
and fail closed when the temporary mode or browser transport cannot be
verified.

For custom local browser queries, write output to a temporary path:

```sh
python3 profile/skills/chatgpt-chat/scripts/chatgpt_conversation.py \
  --prompt "Compare Python dataclasses and Pydantic." \
  --topic "Python data models" --output /tmp/test_chatgpt.yaml
```

Browser-backed helpers can also be checked with `make skills-verify` when the
documented local Playwright MCP prerequisites and authenticated browser session
are available. Keep live verification separate from offline self-tests and do
not use personal workspace data as test fixtures.

### Bundled Dependencies

`make venv` installs the repository's development requirements for local tests.
The derived Hermes image installs only the bundled requirement files explicitly
listed in `docker/Dockerfile`. Adding an arbitrary `requirements.txt` below
`profile/skills/` does not cause Docker or deployment to discover and install
it automatically.

When a bundled skill needs a runtime dependency, update its pinned requirement
file, the explicit Docker build inputs, development requirements when needed,
third-party notices, and tests together. Rebuild the image before expecting the
dependency in Hermes.

## External Skills

External skills do not use bundled `requirements.txt` aggregation and are not
installed into Hermes' global Python environment. Their repository must provide
`openlia-skills.json`; each skill must provide `SKILL.md`. Optional dependencies
use these exact pairs:

- `pyproject.toml` and `uv.lock`
- `package.json` and `bun.lock`

OpenLia audits frozen production dependencies in an isolated one-shot container
and stores a content-addressed environment outside the skill tree. A manifest
`test` argv array is also run in an isolated, networkless container. See the
[external repository guide](EXTERNAL_SKILLS.md) for the schema, commands,
approval requirements, customization flow, isolation guarantees, and current
limitations.

## Safety

- Keep tests deterministic and offline unless they are explicitly live checks.
- Never write tests against `/opt/data/workspace`, active chats, or real user
  records.
- Never commit credentials, protected dotenv files, runtime caches, dependency
  environments, or private repository identifiers.
- Treat skill instructions, scripts, and dependency packages as executable
  supply-chain input. Passing an OpenLia audit is not a source-code review.
- Use temporary output paths for local development and disposable credentials
  for explicit live verification.

## Deployment

Bundled skill changes are synchronized by `openlia update openlia` and follow
the bundled managed/fork migration flow. External repositories are not included
in that release synchronization. Add a source once with `skill-sources add`;
initialized deployments validate and fetch it automatically. `skills install`
and `skills update` fetch and audit automatically, show a commit-bound plan, and
then request confirmation. Explicit `skill-sources check`, `skill-sources fetch`,
and `skills audit` commands are diagnostics. In both workflows, fork a managed
installed skill before intentional local customization so future updates do not
overwrite it. For external skills, rerunning `skills fork` refreshes an existing
fork; the `fork-refresh` command is an advanced alias.
