---
name: inbox-triage
description: Classify captured items into safe next actions.
version: 0.1.0
platforms: [macos, linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, inbox, workflow]
    category: productivity
---

# Inbox Triage

## When to Use

Use when captured notes need a first-pass route without forcing a final filing
decision.

## Procedure

1. Read the supplied inbox records and preserve their identifiers.
2. Run `scripts/triage_inbox.py INPUT.json` for a deterministic suggestion.
3. Review every route, especially `archive` and any item with ambiguous wording.
4. Propose file moves or task creation; apply them only after approval.

Input is a JSON list or `{ "items": [...] }` with optional `id`, `title`,
`content`, `tags`, and explicit `type` fields. The helper is local-only and
does not modify the input.

## Pitfalls

- Keyword classification is a suggestion, not a user decision.
- Do not infer deadlines, owners, or urgency from missing fields.
- Never send a message or create a calendar event during triage.

## Verification

Confirm the output count matches the input count, every item has one route, and
the source inbox is unchanged.
