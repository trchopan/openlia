# OpenLia

> A personal operating system running on Hermes Agent.

OpenLia is a thin operations and setup layer for deploying
[Hermes Agent](https://hermes-agent.nousresearch.com/) as a persistent personal
operating system. The repository defines the workspace model, agent behavior,
skills, service attachments, credentials workflow, and deployment conventions;
Hermes remains the upstream agent runtime.

## Repository Purpose

The first milestone is a reproducible single-user deployment on a local machine
or a remote Linux VM:

- Docker runs the Hermes gateway with persistent state.
- A workspace template organizes personal information around durable concepts.
- A claim ledger records reusable personal context with evidence and lifecycle
  metadata rather than treating every model inference as a fact.
- Hermes skills provide workflows that operate across those concepts.
- Locho attachments provide private access to selected external services.
- Authentication is kept outside Git and supports provider credential rotation.
- A derived image makes `bun` and `uv` available to Hermes and its skills.

This repository is not a fork of Hermes and does not replace Hermes' own
configuration, memory, gateway, or skill systems. It supplies an opinionated
template around them.

## Deployment Shape

```text
OpenLia repository/release
        |
        v
Operator machine
        |
        | openlia CLI: deployment, operations, diagnostics
        +-- Local Docker engine
        |     `-- Docker Compose (runtime plane)
        `-- SSH / Git -> Remote Linux VM
              `-- Docker Compose (runtime plane)

Both runtime planes contain:
  +-- Hermes gateway container
  +-- one Locho attachment container per host
  +-- persistent Hermes data (/opt/data)
  +-- runtime-only secrets
  `-- Personal OS workspace
```

The default deployment uses one Hermes profile and one supervised gateway
container. The dashboard and API remain private by default and are reached
through SSH or a private network such as Tailscale. Locho services are exposed
to Hermes over an internal Docker network, not the public VM interface.

OpenLia itself is the operator control plane, not a long-running Compose
service. The `openlia` CLI runs on the operator machine and either invokes local
operations directly or uses SSH for a remote target. The default Compose stack
contains only the Hermes and Locho runtime services. A future `openlia-tools`
Compose profile may provide a pinned, one-shot diagnostic image, but it must not
be started by normal stack startup or manage updates independently.

The target repository layout is:

```text
docker/                 Derived Hermes image and Compose files
operator/               Typed Go deployment and runtime operations
cmd/operator/           Cross-compiled target operator entrypoint
profile/                Hermes profile template and distribution metadata
profile/skills/         Workflow skills and deterministic helper scripts
workspace-template/     Initial Personal OS directories and instructions
locho/hosts/            Per-host attachments.toml templates
```

Secrets, Hermes state, memories, sessions, logs, OAuth data, and real Locho
capabilities belong only on the VM's runtime storage. They must never be
committed to this repository.

## Why Lia?

**Lia** is a classic Italian form of **Leah**. In Dante's *Purgatorio*, Leah
represents the active life: she gathers flowers and makes a garland, while her
sister Rachel represents the contemplative life.

That is the idea behind OpenLia. It should help turn reflection into movement:
goals into plans, plans into decisions, and decisions into deliberate action.

## Vision

OpenLia should continuously help a person operate their digital life. It can
observe relevant information, remember context, research and reason about
options, make recommendations, and carry out approved actions.

```text
Digital life
     |
  Observe
     |
 Understand
     |
  Remember
     |
   Reason
     |
 Recommend
     |
  +--+----------------+
  |                   |
Wait for approval    Act
                      |
                Update state
```

The goal is not maximum autonomy. The goal is useful autonomy with a clear
approval boundary.

## The Personal Model

OpenLia organizes information around durable concepts rather than around the
services where information happens to live.

```text
PERSONAL MODEL
|-- Inbox
|-- Goals
|-- Areas
|-- Projects
|-- Knowledge
|-- Ideas
|-- Decisions
|-- Monitors
|-- Tasks
|-- Calendar
|-- People
|-- Shopping
|-- Travel
|-- Finance
`-- Archive
```

These concepts are intentionally independent from any particular email
provider, bank, calendar, browser, or note-taking application. Services may
change; the model of the person's life should remain stable.

### Goals

Goals provide the reason behind the work. They connect high-level direction
to concrete outcomes:

```text
Goal: Become proficient in distributed systems
  -> Learning plan
  -> Projects
  -> Tasks
```

### Areas

Areas are the ongoing parts of life that need attention, such as health,
learning, career, family, home, finance, and personal responsibilities.

### Projects

A task is something to do. A project is something to accomplish. Projects can
contain objectives, milestones, tasks, people, documents, decisions, risks,
deadlines, and status.

### Decisions

Decisions are first-class records, not disposable conversations. A decision
can retain its question, context, options, constraints, evidence, trade-offs,
chosen outcome, date, and later result.

### Monitors

Monitors express a desire to watch for change rather than perform a one-time
task. They can track prices, flights, portfolios, news, software releases, job
listings, websites, research topics, subscriptions, or calendar changes.

### Inbox

The inbox is the universal intake point. A person should be able to capture
something without deciding its final category first:

```text
New information
      |
   OpenLia
      |
  +---+---------+----------+
  |             |          |
 Task        Decision   Idea / Research
  |             |          |
Project      Evidence   Knowledge
```

OpenLia can organize and classify captured information later, when there is
enough context to do so well.

### Claim Memory

Reusable personal context belongs in `knowledge/claims/` as a small Markdown
record with YAML front matter containing a stable ID, claim kind, source,
provenance, temporal scope, and status. The claim kind distinguishes `reported`,
`observed`, `inferred`, and `hypothesis`; a confidence score never replaces
evidence. The read-only claim index uses an explicit `--as-of` date so expiry
and review checks are deterministic.

The workspace claim ledger is the canonical long-term record. Hermes runtime
memory may cache claim IDs and summaries for retrieval, but Hermes-only memory
must not override an active workspace claim or become durable truth without a
source reference and review. Conflicting claims remain visible as contested or
superseded records rather than being silently overwritten.

## Cross-Domain Capabilities

Domains describe *what* matters in a person's life. Capabilities describe
*how* OpenLia can work with it.

### Observe

- Read relevant email and messages
- Inspect calendars and files
- Browse websites and connected services
- Collect financial, travel, or research information

### Understand and Remember

- Extract facts, commitments, dates, and relationships
- Preserve reusable claims with source, provenance, temporal scope, and status
- Connect new information to goals, projects, and people
- Maintain useful personal context without forcing premature categorization

### Reason and Recommend

- Research and compare options
- Analyze spending, schedules, and portfolios
- Prioritize work and identify follow-ups
- Forecast, plan, and explain trade-offs

### Act

- Create or update tasks, projects, notes, and calendar events
- Draft messages and documents
- Schedule work and send notifications
- Use browser automation and external APIs when explicitly authorized

Examples of cross-domain workflows include:

```text
Finance + Research              = investment analysis
Shopping + Finance              = purchase decision
Calendar + Projects             = execution planning
Ideas + Knowledge               = idea development
Travel + Finance + Calendar     = trip planning
Email + Tasks + Calendar        = follow-up management
Health + Calendar + Habits      = realistic fitness planning
```

## Review Loops

OpenLia is designed to be useful between questions, not only when prompted.
Periodic reviews keep the personal model current and turn scattered signals
into timely attention.

```text
Daily      Calendar, urgent tasks, follow-ups, important messages, monitors
Weekly     Projects, finance, learning, ideas, shopping watches, next week
Monthly    Spending, portfolio, goals, habits, projects, major decisions
Quarterly  Goals, career, finance, learning, projects, long-term direction
```

## Trust and Approval

OpenLia should be able to do low-risk observation and analysis automatically,
while preserving human control over consequential changes.

| Operation | Default |
| --- | --- |
| Read email or research a product | Automatic |
| Analyze spending or check a portfolio | Automatic |
| Add a calendar event or update a task | Ask |
| Send an email or buy a product | Ask |
| Place a trade or transfer money | Always ask |
| Delete important data | Always ask |

The assistant should explain what it found, what it recommends, and what will
happen before it crosses an approval boundary.

## Workflows, Not Isolated Assistants

OpenLia skills should represent meaningful workflows rather than disconnected
feature areas. Possible workflows include:

- Daily briefing
- Weekly review
- Inbox triage
- Deep research
- Investment research
- Portfolio review
- Spending review
- Personal finance
- Study planning and study sessions
- Idea capture and idea review
- Project review
- Meeting preparation
- Travel planning
- Shopping research
- Price monitoring
- Decision analysis
- Personal review

Each workflow can combine multiple capabilities and domains while keeping the
underlying personal model consistent.

## Project Status

The first implementation is a thin operations layer, not a new agent runtime.
It provides a Go operator CLI, pinned Docker/Compose assets, a file-based
Personal OS template, eight workflow skills, credential rotation, Locho
attachments, and backup/recovery operations for local or remote deployments.

The initial implementation will establish:

1. A durable personal model for goals, areas, projects, knowledge, decisions,
   monitors, tasks, people, and the inbox, with a provenance-aware claim ledger.
2. A versioned Hermes workspace template with `SOUL.md`, `AGENTS.md`, skills,
   and optional review automations.
3. Docker deployment with persistent state, pinned runtime dependencies, and
   operational commands for update, backup, restore, health checks, and
   workspace Git synchronization.
4. Credential rotation for OpenAI-compatible endpoints and GitHub Copilot
   without placing secrets in Git.
5. Locho attachment management for multiple hosts and multiple services per
   host.

Later work can add more integrations, richer interfaces, and additional
automations without changing the underlying Personal OS model.

## Design Principles

- **Life domains over app silos:** finance, learning, travel, and shopping are
  domains, not separate assistants.
- **Goals before tasks:** actions should retain the context of what they serve.
- **Capture first, organize later:** the inbox should make recording frictionless.
- **Decisions deserve memory:** preserve evidence, reasoning, and outcomes.
- **Claims need provenance:** preserve evidence, temporal scope, uncertainty, and
  lifecycle status for reusable personal context.
- **Observe before acting:** recommendations should be grounded in current
  context.
- **Human control matters:** consequential actions require explicit approval.
- **Stable concepts, replaceable integrations:** external services are adapters,
  not the foundation of the personal model.
- **Thin operations layer:** use Hermes features before introducing custom
  services or runtime code.
- **Secrets stay outside Git:** credentials and service capabilities are
  injected at deployment time and rotated operationally.
- **Explicit deployment boundaries:** the VM, Hermes container, Locho
  attachments, and Personal OS workspace have separate responsibilities.

## Name and Direction

OpenLia is meant to be both reflective and practical: a system for thinking
clearly, remembering context, and acting intentionally in the world.

The practical first step is an opinionated Hermes deployment that can be
recreated, updated, backed up, and moved to another VM without losing the
Personal OS structure or its durable state.

## Prerequisites

| Use case | Required runtime |
| --- | --- |
| CLI/static smoke | Go 1.26+, Python 3 with `requirements-dev.txt`, Bash, Docker CLI for Compose validation |
| Local deployment on Linux | Go 1.26+, Docker Engine with Compose v2 |
| Local deployment on macOS | Go 1.26+, Docker Desktop with a Linux engine |
| Remote deployment | Go 1.26+ locally; SSH, Linux, Docker, and Compose v2 on the target |

New deployments use the Go operator and do not require host-target Bash or
Python. Bash and Python remain development and test requirements for the
bundled legacy helpers, smoke runner, and deterministic skill checks. A
release without a target operator artifact automatically uses those legacy
helpers when available.

The first deployment builds pinned Linux images and therefore requires network
access to the configured image registries and release downloads. The pinned
images and bundled binaries support `amd64` and `arm64`. Local Docker roots must
be on a filesystem shared with Docker Desktop on macOS.

## Quick Start

Build the host CLI and the Linux target operators with Go 1.26 or newer:

```sh
make build
./openlia init --local --root "$HOME/.openlia"
```

`make build` writes the uncommitted Linux amd64 and arm64 operator artifacts
under `dist/`; the host CLI includes them in release archives when they are
present. `go build -o openlia .` remains the host-only build for development.

On macOS, Docker Desktop provides the Linux container engine used by the local
deployment. On Linux, a local Docker Engine with Compose v2 is supported.
Remote deployment remains available:

```sh
./openlia init --target user@host --root /srv/openlia
```

`init` installs the pinned OpenLia release at the selected root, initializes the
workspace only when it is empty, starts the Compose stack, and runs health
checks. Provider credentials are never accepted as command-line values.
Configure a protected dotenv source path in the operator config:

```toml
[secrets]
source = "/path/outside/this/repository/hermes.env"
```

The source must already exist as a regular file outside the checkout with mode
`0600`:

```sh
chmod 600 "$HOME/.config/openlia/dev/openlia_dev.env"
```

Local deployments preserve host-file ownership; the Linux containers normalize
their runtime ownership internally. No host-side `chown` to the container UID
is required for local mode.

The configured source is used by `init` and subsequent credential rotations:

```sh
./openlia auth rotate
```

Credential rotation keeps the previous secret in a protected runtime backup so
it can be restored if container recreation fails. Remove the deployment or its
runtime backups when that recovery point is no longer needed.

`init` and `auth rotate` use the configured source; source paths are not accepted
as command-line arguments.

The source file may contain `OPENAI_API_KEY`, numbered OpenAI key siblings,
`OPENAI_BASE_URL`, `OPENLIA_MODEL`, a supported `COPILOT_GITHUB_TOKEN`, or
`OPENLIA_GIT_TOKEN`.
Classic `ghp_*` tokens are not valid for Copilot. Hermes reads the file through
its `secrets.command` source; its values are never printed by OpenLia.

To enable the workspace Git backup, add a repository-scoped GitHub personal
access token to the same protected source. The token must be limited to the
target repository and use a supported GitHub PAT format:

```dotenv
OPENLIA_GIT_TOKEN=github_pat_...
```

Configure the non-secret repository settings during initialization:

```sh
./openlia init --local --root "$HOME/.openlia" \
  --workspace-git-remote https://github.com/OWNER/REPOSITORY.git
```

The remote URL, branch, schedule, and commit identity are stored in the
operator `config.toml`; the PAT remains only in the protected secret source.
OpenLia initializes the workspace Git repository, safely reconciles an existing
remote `main` history, performs the initial push, and enables a no-agent Hermes
pull job. The default schedule is every five minutes. Conflicting histories
stop without discarding either side.

The automatic job only fast-forwards a clean local branch from the remote. It
does not stage, commit, rebase, or push workspace changes, and does not invoke a
model. Use the bundled `workspace-git` skill for status checks, requested
pushes, structural branches, and GitHub pull requests.

The Hermes agent uses `Asia/Ho_Chi_Minh` by default. Set another IANA timezone
per target during initialization:

```sh
./openlia init --target user@host --timezone America/New_York
```

The timezone is stored in the operator `config.toml` and applied to Hermes
through its documented `HERMES_TIMEZONE` setting, with `TZ` also set for
system-level libraries. OpenLia's operational timestamps remain in UTC. When
managing multiple targets, use a separate `OPENLIA_CONFIG` file for each target
so each agent can have its own timezone.

## Operations

```text
openlia status --json
openlia doctor --json
openlia logs --follow
openlia stop
openlia start
openlia restart
openlia uninstall --local --project NAME --root /path
openlia uninstall --target user@host --project NAME --root /path
openlia backup create
openlia backup restore --non-interactive --archive /path/to/backup.tar.gz
openlia workspace git status
openlia workspace git setup
```

`openlia update` is read-only without a component. `openlia update openlia`
synchronizes the Go operator, profile, templates, and bundled skills.
The Hermes and Locho commands reconcile only their pinned runtime boundaries;
they do not silently replace desired digests.

Profile synchronization tracks distribution-owned skill provenance in
`meta/managed/skills/<skill>.json`, including the distribution version, source
identifier, content hash, and timestamps. It updates an unchanged managed
skill, preserves customized skills, and reports untracked existing skills as
`unmanaged` rather than overwriting them. A matching content hash without valid
provenance is still `unmanaged`. Use the JSON result from
`openlia update openlia --json` to inspect these migration statuses.
These provenance records are also the contract for future read-only status,
diff, and explicit migration commands; normal synchronization does not create
or repair missing provenance.

Skills are workflow-oriented rather than domain-specific:

```text
openlia skills list --json
openlia skills show daily-briefing
openlia skills test deep-research
```

## Security Boundaries

- Hermes runs inside the derived image as the upstream unprivileged runtime user.
- `/opt/data` is the only mutable Hermes volume.
- The default Compose stack has no public ports and no Docker socket mount.
- Locho listeners use the private Compose network and are never published.
- Workspace initialization is copy-once; later deployments preserve user files.
- Backups exclude secret files, OAuth state, and Locho capabilities.
- Dangerous unattended actions are denied and skill writes are staged for review.
- Workspace Git uses a repository-scoped GitHub PAT through a mounted askpass
  helper; credentials are not stored in Git remotes or workspace files.
- Automatic workspace pulls are handled by a static no-agent cron script and
  refuse dirty-branch conflicts, instruction-file changes, hard resets, and
  force-pushes.

## Local Verification

```sh
go test ./...
go vet ./...
python3 tests/smoke.py --mode cli
python3 tests/smoke.py --mode local --root /tmp/openlia_smoke
make smoke-local-live \
  OPENLIA_SMOKE_ENV_FILE="$HOME/.config/openlia/dev/openlia_dev.env" \
  OPENLIA_SMOKE_ATTACHMENTS_FILE="$HOME/.config/openlia/dev/locho-attachments.toml" \
  OPENLIA_SMOKE_LOCHO_HOST=genai
docker compose -f docker/compose.yaml config --quiet
```

The CLI smoke gate uses synthetic data. The local deployment smoke starts the
real Compose stack, checks the local lifecycle, and removes its disposable root
when complete. Its root must not already exist; an existing root is never
cleaned automatically. Provider requests, OAuth, Locho connectivity, VM reboot
behavior, external exposure scans, and recovery tests require disposable
credentials or a disposable target. They must be reported as `N/A` when those
prerequisites are absent.

For a credential-backed local deployment that remains available for manual
conversation, use `--mode live --local` and omit `--cleanup`. Telegram checks
are `N/A` without an interactive terminal; they do not claim that Telegram was
tested. See [`tests/README.md`](tests/README.md) for the complete command and
the explicit uninstall command.

For the explicit live Telegram and Personal Finance smoke test, see
[`tests/README.md`](tests/README.md). It requires all deployment paths and
identifiers as arguments and leaves the development target running unless
`--cleanup` is supplied.

## Contributing and License

Development requirements and local checks are documented in
[`CONTRIBUTING.md`](CONTRIBUTING.md). OpenLia source files are available under
the MIT License; see [`LICENSE`](LICENSE). Runtime images and external tools
remain subject to their respective upstream licenses.
