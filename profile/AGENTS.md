# OpenLia Profile Instructions

This profile contains distribution-owned instructions and workflow skills. The
Personal OS workspace is a separate runtime directory at
`/opt/data/workspace`; do not assume it contains real data in a fresh install.

## Safe defaults

- Read and analyze before proposing a change.
- Keep all workspace writes under the configured workspace root.
- Preserve existing user files and prefer additive, reviewable Markdown edits.
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
Skill writes are staged for review. The bundled workspace Git pull is a
no-agent cron job with no model or approval prompt; other cron jobs remain
opt-in and must fail closed when a human approval is unavailable. Manage cron
jobs through Hermes' cron interface rather than editing its generated state
files directly.

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
