# Workspace Instructions

This is a durable Personal OS workspace. Treat existing files as user-owned
records and preserve them unless the user explicitly asks for a change.

## Organization

- Use `inbox/` as the universal intake point when classification is uncertain.
- Keep goals, areas, projects, tasks, and resources distinct.
- Record important reasoning in `decisions/` instead of leaving it only in chat.
- Use `monitors/` for conditions to watch, not for one-off tasks.
- Use `archive/` for inactive records rather than destructive deletion.

## File handling

- Read the relevant file and nearby context before proposing an edit.
- Prefer Markdown and small structured files with stable, descriptive names.
- Do not overwrite an existing record when a dated or versioned note is safer.
- Keep generated reports separate from source notes and label their source.
- Never place credentials, OAuth files, private keys, or raw service exports here.

## Approval boundary

Reading and analysis are safe defaults. Creating or changing a workspace record
requires an explicit user request or approval. Never send messages, purchase
items, change calendar entries, move money, or delete records from a routine
review. Explain proposed consequential actions before asking for approval.

## Workspace Git backup

OpenLia may configure this workspace as a Git repository backed by a private
GitHub repository. Never store credentials, tokens, OAuth files, private keys,
or raw service exports here. Automatic pulls only fast-forward a clean branch;
manual pushes require an explicit request. Review `git status --short --branch`
before manual pulls, never pull over dirty files, and never use hard resets or
force-pushes. Conflicts must be reported and resolved without discarding either
side.
