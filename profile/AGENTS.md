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
- Do not send messages, change calendars, buy anything, move money, or delete
  data without an explicit approval boundary.

Credentials belong in Hermes' runtime secret source or environment, never in
this profile, the workspace template, prompts, helper input, or reports.
Skill writes are staged for review. Cron jobs are opt-in and must fail closed
when a human approval is unavailable. Manage cron jobs through Hermes' cron
interface rather than editing its generated state files directly.
