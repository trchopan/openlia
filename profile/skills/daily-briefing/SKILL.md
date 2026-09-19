---
name: daily-briefing
description: Build a concise read-only briefing from daily inputs.
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

Use for a morning or on-demand snapshot of supplied calendar, task,
follow-up, monitor, and focus data.

## Procedure

1. Gather only the read access needed for the requested briefing.
2. Put normalized data in the JSON shape accepted by
   `scripts/build_briefing.py`.
3. Run the helper and inspect ordering, missing sections, and proposed focus.
4. Report recommendations separately from facts; ask before any update.

The input object may contain `date`, `focus`, `calendar`, `tasks`,
`follow_ups`, and `monitors`. The helper never uses the current clock and never
contacts a service.

## Pitfalls

- A briefing is not permission to change a task or calendar.
- Do not present missing data as an empty day.
- Keep urgent signals distinct from general suggestions.

## Verification

Confirm the report names its input date, shows each supplied section, and makes
no workspace or external service changes.
