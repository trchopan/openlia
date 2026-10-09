---
$schema: ./ingestion-template.schema.json
intake_id: null
source_id: null
version_id: null
captured_at: null
kind: null
operation: extract
requested_outputs: []
status: captured
job_id: null
continuation:
  next_action: null
  conversation_ref: null
  request_ref: null
authorization:
  scope: null
  source: null
artifact_refs: []
extraction_refs: []
candidate_ids: []
record_refs: []
retry_count: 0
last_error: null
unresolved_questions: []
callback:
  event_id: null
  delivery_status: pending
---
# <Ingestion Intake>

## Status

This record reconstructs an ingestion workflow after runtime queue loss. It
must link to durable source and extraction references without copying private
source contents.
