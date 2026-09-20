#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
    printf '%s\n' 'ops tests skipped: Docker engine unavailable'
    exit 0
fi
TEST_ROOT=$(mktemp -d)
trap 'rm -rf "$TEST_ROOT"' EXIT

export OPENLIA_RUNTIME_ROOT="${TEST_ROOT}/runtime"
export OPENLIA_PROJECT_NAME=openlia-test
export OPENLIA_COMPOSE_FILE="${ROOT}/docker/compose.yaml"
export OPENLIA_GENERATED_COMPOSE="${TEST_ROOT}/generated.yaml"
export OPENLIA_DATA_ROOT="${OPENLIA_RUNTIME_ROOT}/hermes"
export OPENLIA_LOCHO_ROOT="${OPENLIA_RUNTIME_ROOT}/locho"
export OPENLIA_SECRET_FILE="${OPENLIA_RUNTIME_ROOT}/secrets/hermes.env"
export OPENLIA_BACKUP_ROOT="${OPENLIA_RUNTIME_ROOT}/backups"
export OPENLIA_META_ROOT="${OPENLIA_RUNTIME_ROOT}/meta"
export OPENLIA_STATE_FILE="${OPENLIA_META_ROOT}/stack-state"
export OPENLIA_LOCAL_MODE=true

mkdir -p "$OPENLIA_RUNTIME_ROOT"
"${ROOT}/ops/bootstrap.sh" --json >/dev/null
[[ -d "${OPENLIA_DATA_ROOT}/workspace/inbox" ]]
[[ -d "${OPENLIA_DATA_ROOT}/workspace/knowledge/claims" ]]
[[ -f "${OPENLIA_DATA_ROOT}/workspace/.gitignore" ]]
[[ -x "${OPENLIA_DATA_ROOT}/scripts/openlia-workspace-git-sync.sh" ]]
claim_skill="${OPENLIA_DATA_ROOT}/skills/claim-review"
claim_marker="${OPENLIA_META_ROOT}/managed/skills/claim-review.json"
[[ -f "${claim_skill}/SKILL.md" ]]
[[ -f "$claim_marker" ]]
profile_sync_output=$("${ROOT}/ops/profile.sh" sync --json)
python3 - "$profile_sync_output" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
assert payload["skills"]["customized"] == 0
assert payload["skills"]["unchanged"] > 0
claim = next(item for item in payload["skills"]["results"] if item["name"] == "claim-review")
assert claim["state"] == "managed"
assert claim["provenance"] == "valid"
assert claim["managed_source"] == "local-checkout"
PY
status_output=$("${ROOT}/ops/skill-status.sh" --skill claim-review --json)
python3 - "$status_output" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
assert payload["action"] == "profile-status"
assert payload["summary"]["managed"] == 1
assert payload["skills"]["results"][0]["action"] == "unchanged"
PY
printf '%s\n' '# Local customization' >>"${claim_skill}/SKILL.md"
profile_sync_output=$("${ROOT}/ops/profile.sh" sync --json)
python3 - "$profile_sync_output" "${claim_skill}/SKILL.md" <<'PY'
import json
import sys
from pathlib import Path

payload = json.loads(sys.argv[1])
assert "claim-review" in payload["skills"]["customized_names"]
assert "# Local customization" in Path(sys.argv[2]).read_text(encoding="utf-8")
PY
status_output=$("${ROOT}/ops/skill-status.sh" --skill claim-review --json)
python3 - "$status_output" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
result = payload["skills"]["results"][0]
assert result["state"] == "customized"
assert result["action"] == "blocked"
assert result["reason"] == "local_modifications_detected"
PY
rm -f "$claim_marker"
profile_sync_output=$("${ROOT}/ops/profile.sh" sync --json)
python3 - "$profile_sync_output" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
assert "claim-review" in payload["skills"]["unmanaged_names"]
PY
human_output=$("${ROOT}/ops/profile.sh" sync)
[[ "$human_output" == *'unmanaged claim-review'* ]]
[[ "$human_output" == *'automatic update blocked'* ]]
deploy_output=$("${ROOT}/ops/deploy.sh" profile --json)
python3 - "$deploy_output" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
assert "profile_sync" in payload
assert "claim-review" in payload["profile_sync"]["skills"]["unmanaged_names"]
PY
rm -rf "$claim_skill"
cp -a "${ROOT}/profile/skills/claim-review" "$claim_skill"
rm -f "$claim_marker"
profile_sync_output=$("${ROOT}/ops/profile.sh" sync --json)
[[ ! -f "$claim_marker" ]]
python3 - "$profile_sync_output" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
assert "claim-review" in payload["skills"]["unmanaged_names"]
PY
source "${ROOT}/ops/lib/common.sh"
printf '%s\n' '# Previous managed copy' >"${claim_skill}/SKILL.md"
previous_hash=$(openlia_directory_sha256 "$claim_skill")
printf '{"schema":1,"skill":"claim-review","distribution":"openlia-personal-os","distribution_version":"0.1.0","source_id":"local-checkout","artifact_path":"profile/skills/claim-review","hash_scope":"skill-files-v1","content_sha256":"sha256:%s","installed_at":"2026-09-20T00:00:00Z","updated_at":"2026-09-20T00:00:00Z"}\n' "$previous_hash" >"$claim_marker"
profile_sync_output=$("${ROOT}/ops/profile.sh" sync --json)
python3 - "$profile_sync_output" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
assert payload["skills"]["updated"] > 0
PY
cmp -s "${ROOT}/profile/skills/claim-review/SKILL.md" "${claim_skill}/SKILL.md"
removed_skill="${OPENLIA_DATA_ROOT}/skills/retired-skill"
mkdir -p "$removed_skill"
printf '%s\n' 'keep retired skill' >"${removed_skill}/SKILL.md"
profile_sync_output=$("${ROOT}/ops/profile.sh" sync --json)
python3 - "$profile_sync_output" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
retired = next(item for item in payload["skills"]["results"] if item["name"] == "retired-skill")
assert retired["state"] == "unmanaged"
assert retired["reason"] == "distribution_removed"
PY
[[ -f "${removed_skill}/SKILL.md" ]]
sentinel="${OPENLIA_DATA_ROOT}/workspace/inbox/sentinel.md"
printf '%s\n' 'keep me' >"$sentinel"
claim_sentinel="${OPENLIA_DATA_ROOT}/workspace/knowledge/claims/sentinel.md"
printf '%s\n' 'keep claim me' >"$claim_sentinel"
"${ROOT}/ops/bootstrap.sh" --json >/dev/null
[[ "$(<"$claim_sentinel")" == 'keep claim me' ]]
"${ROOT}/ops/profile.sh" sync --json >/dev/null
[[ "$(<"$sentinel")" == 'keep me' ]]
"${ROOT}/ops/backup.sh" create --reason test --json >/dev/null
printf '%s\n' 'ops tests passed'
