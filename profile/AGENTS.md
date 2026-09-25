# OpenLia Profile Instructions

This profile contains distribution-owned instructions and workflow skills. The
Personal OS workspace is a separate runtime directory at
`/opt/data/workspace`. Inspect it before relying on its contents; a fresh
installation may contain only starter data.

## Safe defaults

- Read and analyze before proposing a change.
- Keep all workspace writes under the configured workspace root.
- Preserve existing user files and prefer additive, reviewable Markdown edits.
- When creating new domain records in the workspace, follow the starter templates in `workspace/<domain>/<template>.md` (e.g. `goals/goal-template.md`, `monitors/monitor-template.md`, `projects/project-template.md`, `decisions/decision-template.md`, `people/person-template.md`).
- Use the bundled Python helpers only with explicit input paths and inspect
  their output before applying it to the workspace.
- Do not fetch network data from helper scripts; research tools may be used by
  the agent, with source URLs and claims recorded for review.
- Treat `workspace/knowledge/claims/` as the canonical store for durable
  personal claims. Hermes runtime memory is a cache or context layer, not an
  authority. Never promote Hermes-only memory to an active claim without a
  stable source reference and review.
- Do not send messages, change calendars, buy anything, move money, or delete
  data without an explicit approval boundary.

Credentials belong in Hermes' runtime secret source or environment, never in
this profile, the workspace template, prompts, helper input, or reports.
Treat bundled skills as distribution-owned and read-only. Customize them only
through the supported OpenLia fork and migration workflows.
Skill writes are staged for review. Customize bundled skills through
`openlia skills fork`; normal OpenLia updates preserve forked skills. The
protected `openlia-skill-migration` system skill may propose a migration but
must never edit an active skill or its provenance directly. Applying a
migration requires the host `openlia` CLI and explicit confirmation. The
bundled workspace Git pull is a no-agent cron job with no model or approval
prompt; other cron jobs remain opt-in and must fail closed when a human
approval is unavailable. Manage cron jobs through Hermes' cron interface
rather than editing its generated state files directly.

## Workspace Git backup

- The workspace Git remote is configured by OpenLia during `init`; non-secret
  settings belong in the operator `config.toml`.
- The repository PAT belongs only in the protected secret source as
  `OPENLIA_GIT_TOKEN`. Never place it in workspace files, remote URLs, Git
  config, prompts, reports, or command output.
- Use the `workspace-git` skill for manual pushes and structural changes. The
  bundled no-agent cron job performs the routine pull only.
- Do not use hard resets, force-pushes, destructive conflict resolution, or
  pulls over dirty files. Stop and report the exact Git state instead.

## Browser Boundary

Bundled browser support is limited to supervising and relaying a
user-configured Playwright MCP endpoint. External browser workflows remain the
operator's responsibility. A multi-call external browser workflow must first acquire
`openlia_browser_session_request`, pass its returned `lease` value to every
Playwright tool call, renew it with `openlia_browser_session_touch` when needed,
and release it with `openlia_browser_session_release` when finished. Browser
tool calls without a current lease are rejected. Hermes receives direct eager
tools from the managed conservative allowlist; excluded Playwright tools remain
unavailable through the OpenLia relay. Mutating browser tools require a unique
string `operation_id` per lease workflow, and the relay removes it before
forwarding. These relay checks are not a security boundary for clients that
bypass OpenLia and connect to raw Playwright.
