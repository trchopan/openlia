# Personal OS Workspace

This directory is a small, file-based model of a personal operating system. It
is copied into an empty runtime workspace; later profile syncs seed missing
starter files without replacing existing workspace files.

`workspace.yaml` is the user-owned registry for this workspace. It records the
core domains seeded by OpenLia and any approved nested or top-level extensions.
Hermes must read it before proposing a destination. The registry is descriptive:
it does not create directories or authorize writes by itself.

## Canonical directories

- `inbox/`: uncategorized captures waiting for review.
- `goals/`: outcomes and the reason they matter (`goal-template.md`).
- `areas/`: ongoing responsibilities without a fixed end date (`area-template.md`).
- `projects/`: bounded outcomes with milestones, tasks, risks, and decisions (`project-template.md`).
- `knowledge/`: durable notes, references, and research.
- `knowledge/claims/`: reusable personal claim records with structured evidence,
  provenance, temporal metadata, and status
  (`knowledge/claims/claim-template.md`).
- `ideas/`: possible future work and observations (`idea-template.md`).
- `decisions/`: questions, options, evidence, trade-offs, and outcomes (`decision-template.md`).
- `monitors/`: things to watch for change over time (`monitor-template.md`).
- `tasks/`: concrete actions that can be completed (`task-template.md`).
- `calendar/`: planning notes, meeting agendas, and event context (`event-note-template.md`).
- `people/`: relationship context and person records (`person-template.md`).
- `shopping/`: product research and purchase candidates (`item-template.md`).
- `travel/`: trips, itineraries, and travel research (`trip-template.md`).
- `finance/`: budgets, spending notes, and financial reviews (`finance-template.md`).
- `archive/`: completed or inactive material retained for reference.

The default workspace does not include domain-specific subfolders such as
`travel/ideas/` or `learning/topics/`. Start with the core domains and add an
extension only when a recurring need justifies it. For example, an early travel
possibility can live in `ideas/` with travel context; a committed itinerary can
use `travel/`. A learning practice can use an `areas/` record linked to goals,
projects, tasks, and knowledge. A dedicated extension is optional, not automatic.

## Starter template format

Starter files use YAML frontmatter for structured record metadata. Every
frontmatter template declares its JSON Schema in `$schema`, as a local relative
path from that template (for example, `goals/goal-template.md` can use
`$schema: ./goal-template.schema.json`). The referenced schema must remain
inside the workspace root. That schema applies to every non-template Markdown
file in the template directory and its subdirectories. A more-specific nested
template governs its own subtree.

In ordinary starters, a `null` value is an unfilled field to complete when
creating a record; inline comments show allowed values or expected formats.
The `$schema` property is a validator directive and is not record metadata.
Replace the `<...>` text in ordinary template title headings. Users may
customize their workspace copy by editing the template and its referenced
schema together; profile sync preserves those edits. Validate on demand with the
protected `workspace-template-customization` system skill, or run this in
Hermes:

```sh
/opt/hermes/.venv/bin/python /opt/data/skills/workspace-template-customization/scripts/validate_workspace.py \
  --workspace-root /opt/data/workspace \
  --skill-templates-root /opt/data/skills
```

Claim records use the same template-referenced schema workflow as other
workspace domains. Their schema validates frontmatter; the template documents
the Markdown body structure and evidence practices. Quote ISO-8601 dates;
timestamps must include a timezone offset.

## Working rules

Capture first in `inbox/` when the final category is unclear. Link actions to a
project or goal when that context is known. Prefer small Markdown files with
clear dates and titles over opaque databases. Use `knowledge/claims/` for
reusable personal statements in Markdown files with YAML front matter, and
distinguish direct reports from observations and inferences. Move inactive
material to `archive/` and retain it during routine reviews.

Use only registered paths for automatic routing. If a proposed destination is
not registered, keep the item in `inbox/` and propose the extension instead of
creating a directory silently.

## Workspace Growth

When a new nested folder or top-level domain is needed:

1. Explain the recurring use case and why an existing domain is insufficient.
2. Propose the path, purpose, lifecycle, and whether it needs a template/schema.
3. After approval, add the entry to `workspace.yaml`; create only the approved
   directory and starter files.
4. Run the workspace validator and update any affected skill source lists.

Registered extensions are user-owned and survive profile updates. The protected
`workspace-template-customization` system skill validates registry changes but
does not apply them without explicit approval.

Claims are the authoritative workspace memory. Hermes runtime memory may cache
claims for retrieval, but an unreferenced Hermes memory is not a durable fact.
Keep inferred claims as candidates until they have provenance and review.

This template contains no personal records. Keep credentials, tokens, private
exports, session logs, and service caches outside the tracked template.
