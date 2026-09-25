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

### Browser Boundary

OpenLia does not ship browser-backed skills or live browser canaries. The
optional `openlia-browser` package is only a Playwright supervisor and MCP relay;
its lifecycle is tested as part of the runtime package rather than as a skill.
External skills may use an explicitly attached Playwright endpoint, but their
browser behavior is outside the bundled OpenLia skill set and must be reviewed
by the operator. Because the attached browser uses one shared authenticated
profile, an external multi-call workflow must reserve the browser lease through
`openlia_browser_session_request`, pass its lease to every Playwright call,
renew it during long reasoning or human handoff, and release it when finished.
Browser tools are exposed directly and eagerly from the relay's conservative
allowlist rather than through model tool search. Mutating tools require a string
`operation_id`; the relay strips it before forwarding and handles exact
duplicates or uncertain failures per active lease. Evaluation/code execution,
upload/drop, network request or inspection, console, and other opt-in or
dangerous tools remain unavailable through the OpenLia relay by default. The
relay is not a security boundary for an external client that bypasses it and
reaches raw Playwright.

### Bundled Dependencies

`make venv` installs the remaining Python development requirements for local
tests. The MCP relay and supervisor are Bun/TypeScript artifacts. The derived Hermes image installs only the claim
validation requirement explicitly listed in `docker/Dockerfile`; adding an
arbitrary `requirements.txt` below `profile/skills/` does not cause Docker or
deployment to discover and install it automatically.

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
