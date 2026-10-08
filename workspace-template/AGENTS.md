# Workspace Instructions

This is a durable Personal OS workspace. Treat existing files as user-owned
records and preserve them unless the user explicitly asks for a change.

## Organization

- Read `workspace.yaml` before classifying a record. It is the source of truth
  for approved nested folders and top-level workspace extensions.
- Use `inbox/` as the universal intake point when classification is uncertain.
- Keep goals, areas, projects, tasks, and resources distinct.
- Record important reasoning in `decisions/` instead of leaving it only in chat.
- Store reusable personal claims in `knowledge/claims/` with their source,
  provenance, temporal scope, kind, and status, following
  `knowledge/claims/claim-template.md` and the schema referenced in its
  `$schema` frontmatter property.
- Use `monitors/` for conditions to watch, not for one-off tasks.
- Use `archive/` for inactive records rather than destructive deletion.
- Do not create a new folder because one item appears to fit it. Keep the item in
  `inbox/` and propose a registry extension when an existing domain is not a fit.

## File handling

- Read the relevant file and nearby context before proposing an edit.
  - `goals/goal-template.md`: high-level outcomes, rationale, and linked projects.
  - `areas/area-template.md`: enduring life domains, standards, and active projects.
  - `projects/project-template.md`: bounded initiatives with milestones and tasks.
  - `decisions/decision-template.md`: questions, options, trade-offs, and outcomes.
  - `monitors/monitor-template.md`: standing checks, watch URLs, and triggers.
  - `tasks/task-template.md`: concrete physical actions with priorities and context tags.
  - `people/person-template.md`: relationships, important dates, and open loops.
  - `knowledge/claims/claim-template.md`: starter format for durable
    personal claims; create actual records in the same directory.
  - `ideas/idea-template.md`: seeds, opportunities, and exploration questions.
  - `travel/trip-template.md`: itineraries, reservations, and packing lists.
  - `shopping/item-template.md`: product research, price targets, and evaluations.
  - `finance/finance-template.md`: period reviews, spending targets, and budgets.
  - `calendar/event-note-template.md`: event agendas, notes, and follow-up items.
- Prefer Markdown and small structured files with stable, descriptive names.
- For a domain whose template frontmatter declares `$schema`, every new or
  updated Markdown record in that domain must conform to the referenced schema
  as well as the starter template. Run the `workspace-template-customization`
  validator on demand after template, schema, or record changes.
- Treat a claim as a statement to be supported, not as truth merely because an
  assistant generated it. Keep inferred claims as candidates until approved.
- Compare `valid_from`, `valid_until`, and `review_after` dates with the current
  date during reviews; the claims schema validates metadata but does not compute
  lifecycle states, compare interval boundary dates, check duplicate IDs, or
  index records. Use unique IDs and confirm `valid_from` is not after
  `valid_until` when recording a claim.
- Prefer a dated or versioned note when replacing an existing record could lose
  useful history.
- Keep generated reports separate from source notes and label their source.
- Never place credentials, OAuth files, private keys, or raw service exports here.

## Workspace Extensions

- Prefer an existing core domain before proposing an extension.
- A registered extension may be nested under a core domain or be a new
  top-level domain. It must have a stable path, purpose, and lifecycle.
- A routed extension must declare context tags in `workspace.yaml`; this keeps a
  specialized destination from silently replacing a core route.
- Read the extension's template and schema before creating records. Unknown
  directories are proposals, not automatic destinations.

## Workspace and skill links

When referencing or providing links to workspace documents, notes, or skills in user-facing responses or workspace records:
- Format links using canonical `openlia://` URIs:
  - Workspace documents: `[Label](openlia://workspace/<path>)` (e.g. `[Career Goal](openlia://workspace/goals/career.md)`).
  - Skills: `[Label](openlia://skills/<skill-id>)` (e.g. `[Inbox Triage](openlia://skills/inbox-triage)`).
  - Inside workspace markdown files, document-relative links (e.g. `[Specs](./specs.md)`) are also supported.
- Never embed environment-specific HTTP origins or hostnames into workspace records.

## Approval boundary

Reading and analysis are safe defaults. Creating or changing a workspace record
requires an explicit user request or approval. Never send messages, purchase
items, change calendar entries, move money, or delete records from a routine
review. Explain proposed consequential actions before asking for approval.

When the user shares chat excerpts, forwarded messages, or personal announcements (e.g. via Telegram), do not immediately file them into calendar, tasks, or claims. First, stage them into `inbox/` (e.g. `inbox/YYYY-MM-DD-chat-<slug>.md`). Present a concise summary and proposed extractions, and wait for explicit user confirmation or approval before filing. Once approved, disperse into `calendar/`, `tasks/`, or `knowledge/claims/`.

## Claim Memory

The workspace claim ledger is the authoritative long-term memory for reusable
personal context. Hermes runtime memory may cache claim IDs and summaries, but it
must not override active workspace claims or turn an unreferenced inference into
a fact. Preserve conflicting claims as contested or superseded records instead
of silently overwriting them.

## Workspace Git history

OpenLia maintains this workspace as a local Git repository for history tracking.
After an approved, coherent workspace update, stage only the intended files and
create a concise `backup:` commit. Do not create empty commits or configure Git
remotes; use OpenLia's encrypted backup mechanism for recovery. Never store
credentials, tokens, OAuth files, private keys, or raw service exports here.
Never use Git network operations, hard resets, `git clean`, or destructive
conflict handling.
