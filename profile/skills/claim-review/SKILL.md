---
name: claim-review
description: Review durable personal claims with explicit evidence and uncertainty.
version: 0.1.0
platforms: [macos, linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, memory, claims, provenance]
    category: productivity
---

# Claim Review

## When to Use

Use when a durable personal statement may be useful across future conversations,
such as a reported plan, a preference, an observation, or an inference.

## Canonical Storage

Store approved claims under `knowledge/claims/` as small Markdown records with
YAML front matter. The workspace claim ledger is the authoritative long-term
record. Hermes runtime memory may cache a claim, but it must not be treated as
authoritative without a claim identifier and provenance reference.

## Claim Fields

Every claim must include:

- `id` or `claim_id`: stable, unique identifier.
- `claim` or `statement`: the statement in plain language.
- `kind`: `reported`, `observed`, `inferred`, `hypothesis`, or `hypothetical`.
- `source`: the origin type and stable reference (or descriptive string).
- `provenance`: evidence references and source context (or descriptive string).
- `asserted_at`, `observed_at`, or `first_recorded`: when the evidence was made or observed.
- `status`: `candidate`, `active`, `stale`, `contested`, `superseded`, `retracted`, or `rejected`.

The front matter must be a single YAML mapping. Use quoted dates or ISO-8601
timestamps; the validator normalizes YAML date scalars before checking them.

Use `valid_from`, `valid_until`, `temporal_scope`, and `review_due` (or `review_after`)
when the statement can change over time. Use `confidence` (`low`, `medium`, `high`, or a number
between 0 and 1) with an explicit basis. For inferred or hypothetical claims, explicit
evidence and review are required before becoming `active`.

## Procedure

1. Separate a direct report, an observation, an inference, and a hypothesis.
2. Preserve the original evidence reference; do not represent an assistant
   inference as a user statement.
3. Create a Markdown record from `templates/claim-record.md` with YAML front
   matter.
4. Run `scripts/validate_claims.py CLAIM.md` and inspect every error before
   proposing a workspace write.
5. Keep new inferences as `candidate` until the person approves or corrects them.
6. Create or update the Markdown record only after explicit approval.
7. If evidence conflicts, preserve both records and mark the relevant claim
   `contested` or `superseded`; do not silently overwrite history.

Run `scripts/index_claims.py /opt/data/workspace --as-of YYYY-MM-DD` for a
read-only view of active, stale, expired, contested, and invalid records.

## Retrieval Rules

- Retrieve `active` claims that are not past `valid_until` by default.
- Label `reported`, `observed`, and `inferred` claims in reasoning and responses.
- Prefer recent, directly sourced claims over stale or inferred context.
- Treat Hermes-only memory without a claim reference as untrusted context.
- Never use an inferred claim alone to justify a consequential action.

## Pitfalls

- A confidence score does not make a claim true or current.
- A source type is not provenance; retain the specific evidence reference.
- Do not copy raw service exports, credentials, or sensitive message content into
  a claim record when a stable reference is sufficient.
- Do not bulk-convert existing workspace notes into claims without review.

## Verification

Confirm every proposed claim has valid YAML front matter, a stable ID, evidence
reference, temporal meaning, and explicit status. Confirm inferred claims remain
candidates until approved and that the original source record was not overwritten.
