# Personal OS Workspace

This directory is a small, file-based model of a personal operating system.
It is copied into a runtime workspace only when that workspace is empty.

## Canonical directories

- `inbox/`: uncategorized captures waiting for review.
- `goals/`: outcomes and the reason they matter (`goal-template.md`).
- `areas/`: ongoing responsibilities without a fixed end date (`area-template.md`).
- `projects/`: bounded outcomes with milestones, tasks, risks, and decisions (`project-template.md`).
- `knowledge/`: durable notes, references, and research.
- `knowledge/claims/`: reusable personal claims with explicit evidence,
  provenance, temporal scope, and status (`claim-record.md`).
- `ideas/`: possible future work and observations (`idea-template.md`).
- `decisions/`: questions, options, evidence, trade-offs, and outcomes (`decision-template.md`).
- `monitors/`: things to watch for change over time (`monitor-template.md`).
- `tasks/`: concrete actions that can be completed (`task-template.md`).
- `calendar/`: planning notes, meeting agendas, and event context (`event-note-template.md`).
- `people/`: relationship context, commitments, and follow-ups (`person-template.md`).
- `shopping/`: product research and purchase candidates (`item-template.md`).
- `travel/`: trips, itineraries, and travel research (`trip-template.md`).
- `finance/`: budgets, spending notes, and financial reviews (`finance-template.md`).
- `archive/`: completed or inactive material retained for reference.

## Working rules

Capture first in `inbox/` when the final category is unclear. Link actions to a
project or goal when that context is known. Prefer small Markdown files with
clear dates and titles over opaque databases. Use `knowledge/claims/` for
reusable personal statements in Markdown files with YAML front matter, and
distinguish direct reports from observations and inferences. Move inactive
material to `archive/`; do not delete it as part of a routine review.

Claims are the authoritative workspace memory. Hermes runtime memory may cache
claims for retrieval, but an unreferenced Hermes memory is not a durable fact.
Inferred claims require provenance and review before becoming active records.

This template contains no personal records. Keep credentials, tokens, private
exports, session logs, and service caches outside the tracked template.
