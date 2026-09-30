---
name: workspace-organize
description: Audit workspace integrity, detect orphan or stale items, and propose clean-up actions.
version: 0.1.0
platforms: [macos, linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, workspace, audit, hygiene]
    category: productivity
---

# Workspace Organize Scout

## When to Use

- Use when auditing workspace health, integrity, and organizational consistency.
- Use on-demand when the user asks to clean up, review, or organize the workspace.
- Use periodically to identify unfiled intake captures, broken references, stale claims, and completed projects pending archive.

## Reading Scope

Read relevant sources across the workspace:
- `AGENTS.md` and domain starter templates.
- Canonical 15 domain directories (`inbox/`, `goals/`, `areas/`, `projects/`, `knowledge/`, `ideas/`, `decisions/`, `monitors/`, `tasks/`, `calendar/`, `people/`, `shopping/`, `travel/`, `finance/`, `archive/`).
- Prior organizing reports (e.g. in `inbox/workspace-organize-*.md` or `archive/`) to avoid re-proposing resolved or rejected suggestions.
- Recent Git history (`git log -n 20 --stat`) to see recently modified or added files.

## Operating Boundary (Read-Only Scout)

- **Strictly read-only audit**: Never move, edit, rename, archive, or delete any source record during the scout run.
- **No record invention**: Never fabricate goals, projects, tasks, decisions, claims, or calendar entries.
- **No autonomous git changes**: Never commit, push, pull, or alter Git state during an audit.
- **Fail-closed proposal gate**: Every suggested file move or archive action must be proposed to the user and await explicit confirmation.

## Audit Procedure

1. **Deterministic Static Audit**:
   - Run `scripts/audit_workspace.py /opt/data/workspace` (or current workspace directory) to obtain structural diagnostics.
   - Detect stray root files, unfiled inbox notes, broken relative links, completed projects, and expired claims.

2. **Contextual & Semantic Review**:
   - Read cross-references in active `decisions/` and `goals/` to ensure evidence files exist.
   - Check for duplicate or overlapping research notes that lack cross-links or index notes.
   - Inspect note lifecycle states (e.g. approved chat reviews in `inbox/` that should be archived or retained with explicit status).
   - Verify metadata consistency (e.g. numbering across learning tracks, dated ledgers vs standalone event notes).

3. **Formulate Proposals**:
   - Assign a stable identifier: `O-YYYY-MM-DD-NNN`.
   - Specify:
     - **Area/file**: Target file path or directory.
     - **Type**: `stray-root-file` | `inbox-lifecycle` | `broken-link` | `evidence-provenance` | `duplicate-overlap` | `metadata-inconsistency` | `completed-project`.
     - **Observation**: Objective facts observed in specific files or lines.
     - **Inference**: Why this observation suggests an organizational issue.
     - **Suggested action**: Proposed concrete atomic action (for user review only).
     - **Confidence**: `high` | `medium` | `low`.
     - **Priority**: `high` | `medium` | `low`.
     - **Risk / reason for review**: Why human judgment is needed before acting.
     - **Status**: `proposed`.
   - Limit to at most 15-20 high-value proposals per report. If no issues are found, state clearly that the workspace is clean.

4. **Dedicated Section: Items Not Proposed**:
   - Explicitly list areas or files inspected but intentionally omitted due to insufficient evidence (e.g. empty domains that are intentionally fresh, external web links, or exploratory research).

5. **Delivery**:
   - Stage the full report into `inbox/workspace-organize-YYYY-MM-DD.md`.
   - Send a concise 3-5 bullet summary to chat in the user's configured output language with a canonical workspace link:
     `[Workspace Organize Report](openlia://workspace/inbox/workspace-organize-YYYY-MM-DD.md)`.
   - Do NOT embed environment-specific HTTP origins.

6. **Execution After Approval**:
   - Only execute file moves, archives, or link updates after the user responds with explicit confirmation (e.g. "Approve all" or "Approve O-2026-09-29-001").
   - After applying approved updates, stage changes and make a concise Git commit (`backup: ...`).

## Verification

Confirm that:
1. Static script `scripts/audit_workspace.py` runs without error.
2. The report is staged in `inbox/workspace-organize-YYYY-MM-DD.md`.
3. No source files were altered or moved without user approval.
4. The user received a concise chat summary with a valid clickable link.
