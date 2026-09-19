---
name: weekly-review
description: Review projects, tasks, and decisions without acting.
version: 0.1.0
platforms: [macos, linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, review, weekly]
    category: productivity
---

# Weekly Review

## When to Use

Use at the end or start of a week to reduce open loops and choose a small set
of outcomes for the next period.

## Procedure

1. Gather current project, task, and decision records as read-only inputs.
2. Run `scripts/review_week.py INPUT.json`.
3. Check completed work, active projects, open tasks, and unresolved decisions.
4. Draft a short next-week list and ask before changing workspace records.

The input object accepts `week`, `projects`, `tasks`, and `decisions` lists.
Statuses are compared literally and the output is deterministic.

## Pitfalls

- Do not equate activity with progress or add tasks to fill space.
- Do not close a task or decision merely because it is old.
- A review does not authorize messages, purchases, or scheduling.

## Verification

Confirm all supplied open loops are represented, completed items are separated,
and no source file was changed.
