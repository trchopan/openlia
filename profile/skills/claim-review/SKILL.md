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

Every record uses one strict schema. Its YAML front matter contains metadata;
the human-readable claim is the non-empty text under exactly one `## Claim`
heading in the Markdown body. `claim` and `statement` are not frontmatter fields.

Required frontmatter fields:

- `id`: stable, unique identifier.
- `kind`: `reported`, `observed`, `inferred`, `hypothesis`, or `hypothetical`.
- `status`: `candidate`, `active`, `stale`, `contested`, `superseded`, `retracted`, or `rejected`.
- `source`: an object with non-empty `type` and stable-reference `ref` strings.
- `provenance`: an object with a non-empty list of `evidence_refs` and a list
  of `derived_from` claim IDs (`[]` when the claim is not derived from another claim).
- `asserted_at` or `observed_at`: when the source asserted the claim or the
  observation occurred.

Optional frontmatter fields are `valid_from`, `valid_until`, `review_after`,
`confidence`, `confidence_basis`, `reviewed_at`, `reviewed_by`, and `supersedes`.
These keys may be present with `null` values. Dates are ISO-8601 dates or
timestamps; quote date values in YAML. Date-only values are preferred for
validity and review boundaries, and timestamps without a timezone are treated as
UTC.
`valid_from` and `valid_until` describe when the claim is true in the world,
`review_after` is the date to review it, and `reviewed_at` records when review
occurred. The index evaluates these boundaries by calendar date, with
`valid_until` inclusive.

`confidence` is `null`, `low`, `medium`, `high`, or a number from 0 through 1.
Provide a non-empty `confidence_basis` whenever confidence is supplied. Inferred,
hypothesis, and hypothetical claims require both confidence and its basis. They
also require `reviewed_at` and `reviewed_by` before their status can be `active`.
Unknown frontmatter keys are validation errors; put explanatory material under
`## Evidence` or `## Notes` instead.

## Procedure

1. Separate a direct report, an observation, an inference, and a hypothesis.
2. Preserve the original evidence reference; do not represent an assistant
   inference as a user statement.
3. Create a Markdown record from `templates/claim-record.md`, filling its
   required metadata and writing the claim under `## Claim`.
4. Run `scripts/validate_claims.py CLAIM.md` and inspect every error before
   proposing a workspace write.
5. Keep new inferences as `candidate` until the person approves or corrects them.
6. Create or update the Markdown record only after explicit approval.
7. If evidence conflicts, preserve both records and mark the relevant claim
   `contested` or `superseded`; do not silently overwrite history.

Run `scripts/index_claims.py /opt/data/workspace --as-of YYYY-MM-DD` for a
read-only view of active, stale, expired, contested, and invalid records.

## Legacy Record Migration

The canonical schema is a deliberate hard break; legacy records are not silently
accepted or rewritten. Run the validator to see unsupported fields and their
migration hints. Migrate each record by reviewing its meaning rather than
blindly renaming values:

| Legacy field or format | Canonical migration |
|---|---|
| `claim_id` | Rename to `id`. |
| Frontmatter `claim` or `statement` | Move the text to the body under `## Claim`, then remove the field. Rename an old `## Statement` heading to `## Claim`. |
| `first_recorded` | Map to `asserted_at` or `observed_at`, according to what the date means. |
| `last_reviewed` | Rename to `reviewed_at`. |
| `review_due` | Rename to `review_after`. |
| String `source` | Create `source.type` and `source.ref`; choose the type and stable reference from the actual source. |
| String `provenance` | Create `provenance.evidence_refs` and `provenance.derived_from`; preserve references and do not invent evidence. |
| `temporal_scope` | Map its actual meaning to `valid_from`, `valid_until`, or `review_after`, or retain explanatory context under `## Notes`. |
| `related_claims` | Review each relation and move derivation links to `provenance.derived_from`; use `supersedes` only when one claim replaces another. |

Replace old kind/status choice-list placeholders with one allowed value. Preserve
uncertain or unmappable details in `## Notes` until they can be reviewed. The
workspace starter template is at `workspace-template/knowledge/claim-record.md`,
outside the directory scanned for claim records.

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
