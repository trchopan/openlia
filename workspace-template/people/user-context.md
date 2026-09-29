# User Context

> **Role:** operational context used by assistants and workflows. Schedule and communication preferences are canonical in [[preferences]]; identity, location, office, and commute context are canonical in [[profile]].

## Timezone
`Asia/Ho_Chi_Minh`

## Operational boundaries
- Follow fail-closed defaults for actions requiring approval (calendar creation, messaging external people, file deletion).
- Separate facts from inferences and missing evidence in all summaries.
- Maintain coherent local Git commits with `backup:` prefix, but never push to remote without explicit approval.
