# Workspace Reports

This directory stores machine-generated reports produced by scheduled cron jobs, scout audits, and periodic reviews.

## Subdirectories

- `daily-briefing/`: Daily work and personal status briefings generated each morning (e.g. `YYYY-MM-DD.md`).
- `workspace-organize/`: Automated scout reports auditing workspace consistency, broken references, and unfiled items.

## Rules
- Machine-generated reports must be kept separate from human source records.
- Reports are read-only snapshot outputs and do not replace primary records in `calendar/`, `tasks/`, `projects/`, or `knowledge/claims/`.
