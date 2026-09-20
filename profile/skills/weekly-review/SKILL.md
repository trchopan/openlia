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

1. Gather current project, task, decision, and claim records as read-only inputs.
2. Run `scripts/review_week.py INPUT.json`.
3. Check completed work, active projects, open tasks, unresolved decisions, and
   claims requiring review. Prepare claim input with
   `claim-review/scripts/index_claims.py` using an explicit `--as-of` date.
4. Draft a short next-week list and ask before changing workspace records.

The input object accepts `week`, `projects`, `tasks`, `decisions`, and `claims`
lists. Claim records should come from the read-only claim index. Statuses are
compared literally and the output is deterministic.

## Pitfalls

- Do not equate activity with progress or add tasks to fill space.
- Do not close a task or decision merely because it is old.
- Do not activate, retract, or supersede a claim during a review without approval.
- A review does not authorize messages, purchases, or scheduling.

## Verification

Confirm all supplied open loops and claim review items are represented,
completed items are separated, and no source file was changed.
