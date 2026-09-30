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

Use for a morning or on-demand briefing that synthesizes calendar events, active tasks, monitors, recent changes, and personal priorities into an actionable snapshot.

## Reading Scope

Read relevant sources across the workspace:
- `calendar/`: upcoming events, deadlines, and agendas.
- `tasks/`: current todos, priority queues, and pending actions.
- `projects/` & `goals/`: active milestones and focus areas.
- `decisions/`: recent architectural or personal decisions.
- `monitors/`: standing conditions or watch items.
- `knowledge/claims/`: durable personal claims and context.
- `inbox/`: pending captures or reviews.
- Recent Git history (`git log`): changes since the previous briefing.

## Report Structure (`inbox/daily-briefing-YYYY-MM-DD.md`)

Write a dedicated dated markdown report containing:

### 1. Today
- Evidenced calendar events, appointments, and deadlines.
- Active monitors or external conditions (holidays, weather, etc.).
- If data is absent, explicitly state that no corresponding calendar or task records are available  -  never present it as an empty day.

### 2. What Changed
- Categorize recent changes into:
  - **Facts**: verified records created, edited, or approved with source paths.
  - **Signal**: emerging patterns or operational loops needing attention.
  - **Missing evidence**: unverified inferences or missing follow-ups.

### 3. Important-Urgent Matrix
Rank up to 6 evidenced items across:
- **Do first**: Important + Urgent
- **Schedule**: Important, not urgent
- **Delegate / Coordinate**: Urgent, less critical; requires clear owner
- **Defer / Drop**: Not important, not urgent; suggest deferral without dropping commitments

## Delivery (e.g. Telegram / Chat)

- Never send the entire markdown report to the chat.
- Reply with a concise 3-6 bullet summary focusing on immediate priorities in the user's configured output language.
- Include a canonical workspace link:
  `[Daily briefing YYYY-MM-DD](openlia://workspace/inbox/daily-briefing-YYYY-MM-DD.md)`
- Do not embed environment-specific HTTP origins.

## Pitfalls

- A briefing is read-only: never create tasks, modify calendar entries, or send messages during a briefing.
- Do not fabricate deadlines, progress, or priority levels without evidence.

## Verification

Confirm the report is written to `inbox/daily-briefing-YYYY-MM-DD.md`, contains all 3 sections with explicit source citations, and the chat response includes a valid clickable URL.
