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

- Use when captured notes or batch records need a first-pass route without forcing a final filing decision.
- Use when the user shares forwarded messages, chat excerpts, or announcements (e.g. via Telegram) that contain events, tasks, health context, or personal facts.

## Chat Intake & Review Procedure (Conversational Mode)

1. Receive the unstructured text, message, or chat excerpt.
2. Create or stage a review document under `inbox/YYYY-MM-DD-chat-<slug>.md`.
3. Extract and organize candidate items:
   - **People & Ownership**: differentiate user, spouse/family, colleagues, and external persons.
   - **Proposed Tasks**: concrete physical next actions with owners and optional due dates.
   - **Proposed Events**: title, date/time, participants, location.
   - **Proposed Durable Claims**: candidate statements with explicit kind, source, and evidence.
   - **Unclear / Needs Confirmation**: missing times, ambiguous dates, or unverified facts.
4. Reply to the user with a concise summary (3-5 bullet points) and a clickable workspace link (`[Review Title](openlia://workspace/inbox/YYYY-MM-DD-chat-<slug>.md)`).
5. Explicitly request confirmation or approval (e.g. "Approved", "Confirmed").
6. **Wait for explicit approval** before creating records in `calendar/`, `tasks/`, or `knowledge/claims/`.
7. Once approved:
   - Create or update the target records in `calendar/`, `tasks/`, or `knowledge/claims/`.
   - Update `Status: approved` and record approval timestamp in the chat review file.
   - Stage changes and make a concise local Git commit (`backup: ...`).

## Batch JSON Triage Procedure (Structured Mode)

1. Read the supplied inbox records and preserve their identifiers.
2. Run `scripts/triage_inbox.py INPUT.json` for a deterministic suggestion.
3. Review every route, especially `archive` and any item with ambiguous wording.
4. Propose file moves or task creation; apply them only after approval.

Input is a JSON list or `{ "items": [...] }` with optional `id`, `title`,
`content`, `tags`, and explicit `type` fields. The helper is local-only and
does not modify the input.

## Pitfalls

- Never create calendar events, tasks, or permanent claim records directly from an unreviewed chat message without user approval.
- Do not guess or infer missing dates, medical interpretations, or deadlines; mark them explicitly in `Unclear / Needs Confirmation`.
- Do not embed environment-specific HTTP origins; use canonical `openlia://` workspace URIs.

## Verification

Confirm that:
1. Every candidate chat intake has a staged review file in `inbox/`.
2. Consequential calendar/task/claim writes occur ONLY after explicit approval.
3. Source review documents are preserved for provenance.
