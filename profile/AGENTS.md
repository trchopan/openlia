# OpenLia Profile Instructions

This profile contains distribution-owned instructions and workflow skills. The
Personal OS workspace is a separate runtime directory at
`/opt/data/workspace`; do not assume it contains real data in a fresh install.

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
Bundled skills are distribution-owned and read-only; never attempt autonomous background curation or patching (`skill_manage`) on them.
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

## External AI Research & Mandatory Temporary Chat Gatekeeper

- Whenever the user asks to query, research, or converse with **ChatGPT** (`chatgpt.com`) or **Google Gemini** (`gemini.google.com`):
  - **MANDATORY**: You MUST execute through the dedicated automated scripts. NEVER attempt manual, low-level browser tool loops (`browser_type`, `browser_click`).
  - **For ChatGPT**: Submit `bun /opt/data/skills/browser-pilot/scripts/openlia_job.ts submit chatgpt-chat --prompt "<prompt>" --topic "<topic>"`, then poll the returned job ID with `status` and fetch it with `result`.
  - **For Gemini**: Submit `bun /opt/data/skills/browser-pilot/scripts/openlia_job.ts submit gemini-chat --prompt "<prompt>" --topic "<topic>"`, then poll the returned job ID with `status` and fetch it with `result`.
  - **Zero-Tolerance Temporary Chat**: Both scripts automatically enforce the zero-retention Temporary Chat gatekeeper before submitting prompts, and halt immediately if unverified.
  - **Authentic Markdown & Export**: Both scripts automatically intercept authentic Markdown, sanitize citations, and save standardized YAML files with timestamp prefixes to `workspace/knowledge/<platform>/YYYYMMDD_HHMMSS_<slug>.yaml`.

## Attached Playwright MCP

When a `playwright-browser` attachment is configured, Hermes' native browser
toolset is disabled and the attached MCP tools are the only generic browser
surface. Use the registered `mcp__openlia_playwright__browser_*` tools for Maps,
shopping, and ordinary web navigation. Do not invent terminal scripts such as
`maps_client.py`, and do not use the queued OpenLia job client for generic web
navigation; that client is only for the ChatGPT/Gemini workflows above.
