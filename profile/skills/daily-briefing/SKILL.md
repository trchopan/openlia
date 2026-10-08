---
name: daily-briefing
description: Collect workspace sources and prepare an evidence-based daily briefing.
version: 0.1.0
platforms: [macos, linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, briefing, daily]
    category: productivity
---

# Daily Briefing

## When to Use

Use for a morning or on-demand briefing that synthesizes calendar events,
active tasks, monitors, recent changes, and personal priorities into an
actionable snapshot.

Read `workspace.yaml` first when it exists. The registry controls which core
and user-registered extension paths are included. A registered extension is
included only when it opts into briefings; an unregistered directory is never
silently scanned.

## Gather Sources

Run the bundled collector with the explicit workspace root and local briefing
date:

```sh
python scripts/build_briefing.py --collect \
  --workspace-root /opt/data/workspace \
  --date YYYY-MM-DD
```

The collector prints a compact, read-only source packet. Without a registry it
uses the standard source domains: `calendar/`, `tasks/`, `projects/`, `goals/`,
`decisions/`, `monitors/`, `knowledge/claims/`, and `inbox/`. With a registry it
uses only entries whose `include_in_briefing` is true, including registered
extensions. It uses structured dates and statuses where available, includes
active records and recent open loops, and reports missing source directories
separately from directories with no matching records. By default it includes
calendar and due-date items through the next seven days, recent decisions and
inbox captures, and up to 40 source records within an 18,000-character packet.
Increase these limits only when the packet reports omitted records.

The collector also reads local Git history since the latest earlier daily
briefing, or since the previous day when there is no earlier report. Pass
`--since YYYY-MM-DD` to override that baseline. It performs no network access
and does not write to the workspace.

Use the packet to decide which full source records need reading. Read those
records directly before making a consequential claim; the packet excerpts are
discovery aids, not substitutes for evidence. Do not treat missing or
unrecognized metadata as proof that a record is irrelevant. Extension records
without task or calendar semantics are treated as recent signals, not as
commitments.

## Report Structure

Use `templates/briefing.md` as the report skeleton and write the completed
report directly to `inbox/daily-briefing/YYYY-MM-DD.md`. Create the
`inbox/daily-briefing/` directory when it does not exist. The template headings
are the report contract. Replace the starter prompts with report content while
preserving the template's heading structure:

1. **Today**: focus, evidenced calendar events, tasks, follow-ups, and active
   monitors. If no calendar or task records are available, say so explicitly;
   do not imply that the day is empty.
2. **What Changed**: separate verified **Facts**, possible **Signal**, and
   **Missing evidence**. Keep inferences labeled and cite their supporting
   records.
3. **Important-Urgent Matrix**: rank up to six evidenced items under **Do
   first**, **Schedule**, **Delegate / Coordinate**, and **Defer / Drop**. Name
   a clear owner for delegated work. Preserve commitments; do not recommend
   dropping them without evidence.

Read the full source records needed to support the report, then fill the
template directly. Cite factual items with canonical workspace links such as
`[Task](openlia://workspace/tasks/write-outline.md)`. Do not invent dates,
status, progress, priority, owners, or external conditions. When a field is
absent, identify it as missing evidence. Do not use `build_briefing.py` to
render or write the report; it is a source collector only.

After writing the report, verify that the file exists at the nested dated path,
all template headings remain present, every factual item has a source citation,
and no starter prompt remains without either content or an explicit statement
that the corresponding evidence is unavailable.

## Delivery

- By default, save the dated report to
  `inbox/daily-briefing/YYYY-MM-DD.md`.
- If the caller explicitly requests read-only/no-write delivery (including a
  scheduled run), return the structured report in chat and do not write a
  workspace report.
- For a saved report, return 3–6 concise bullets in the user's configured
  output language and link to
  `[Daily briefing YYYY-MM-DD](openlia://workspace/inbox/daily-briefing/YYYY-MM-DD.md)`.
- Send the full structured report in chat only when the caller requests
  no-write delivery or explicitly asks for the entire report. Do not link to a
  report file when no file was written.
- Do not embed environment-specific HTTP origins.

## Safety

A briefing is read-only with respect to source records: never create tasks,
modify calendar entries, send messages, or change source documents. A requested
dated report is the only workspace file written. Review the completed report
for the template headings, explicit citations, and unsupported claims before
delivery.
