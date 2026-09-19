---
name: project-review
description: Assess project health and surface next actions.
version: 0.1.0
platforms: [macos, linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, projects, review]
    category: productivity
---

# Project Review

## When to Use

Use when a project needs a status check grounded in its objective, milestones,
tasks, risks, and decisions.

## Procedure

1. Read one project record and its directly linked records.
2. Run `scripts/project_report.py PROJECT.json --format markdown`.
3. Check whether the objective, status, overdue flags, and next actions are
   still accurate.
4. Recommend one next action or an explicit pause; ask before editing records.

The helper calculates milestone completion only from explicit statuses and
sorts open tasks by priority, due value, and title. It does not infer dates.

## Pitfalls

- Percentage complete is a signal, not proof of outcome.
- Do not hide risks or convert an unresolved decision into a task silently.
- Never close, delete, or re-scope a project without approval.

## Verification

Confirm the objective is present, open tasks are listed, and the input project
record remains unchanged.
