#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

json=false
selected_skill=''
while (($#)); do
    case "$1" in
        --json) json=true ;;
        --skill)
            (($# >= 2)) || { printf '%s\n' 'openlia: --skill requires a name' >&2; exit 2; }
            selected_skill=$2
            shift
            ;;
        -h|--help)
            printf '%s\n' 'Usage: skill-status.sh [--skill NAME] [--json]'
            exit 0
            ;;
        *) printf 'openlia: unknown skill status option %s\n' "$1" >&2; exit 2 ;;
    esac
    shift
done

openlia_validate_paths
openlia_require_command python3
if [[ -n "$selected_skill" ]]; then
    openlia_require_safe_component "$selected_skill" skill
fi
[[ -d "$OPENLIA_DATA_ROOT/skills" ]] || openlia_die 'runtime skills directory is missing'
[[ -d "$OPENLIA_META_ROOT/managed/skills" ]] || openlia_die 'skill provenance directory is missing'

distribution_version=$(python3 - "${OPENLIA_REPO_ROOT}/release/manifest.json" <<'PY'
import json
import sys

try:
    with open(sys.argv[1], encoding="utf-8") as handle:
        value = json.load(handle).get("openlia")
except (OSError, ValueError, TypeError):
    value = None
print(value if isinstance(value, str) and value else "unknown")
PY
)
distribution_source='local-checkout'
if [[ -f "${OPENLIA_REPO_ROOT}/release.sha256" ]]; then
    release_hash=$(awk 'NR == 1 { print $1 }' "${OPENLIA_REPO_ROOT}/release.sha256")
    if [[ -n "$release_hash" ]]; then
        distribution_source="release:${release_hash}"
    fi
fi

read_skill_metadata() {
    local metadata=$1
    local skill_name=$2
    local legacy_marker=${metadata%.json}.sha256
    if [[ ! -f "$metadata" ]]; then
        if [[ -f "$legacy_marker" ]]; then
            printf '%s\n' 'unmanaged||||||legacy_provenance'
        else
            printf '%s\n' 'unmanaged||||||metadata_missing'
        fi
        return
    fi
    python3 - "$metadata" "$skill_name" <<'PY'
import json
import re
import sys

path, expected_skill = sys.argv[1:]
try:
    with open(path, encoding="utf-8") as handle:
        data = json.load(handle)
except (OSError, ValueError):
    data = None

required = ("distribution", "distribution_version", "source_id", "artifact_path", "hash_scope", "content_sha256", "installed_at", "updated_at")
valid = isinstance(data, dict) and type(data.get("schema")) is int and data.get("schema") == 1
valid = valid and data.get("skill") == expected_skill
valid = valid and all(isinstance(data.get(field), str) and data[field] for field in required)
valid = valid and data.get("distribution") == "openlia-personal-os"
valid = valid and data.get("artifact_path") == "profile/skills/" + expected_skill
valid = valid and data.get("hash_scope") == "skill-files-v1"
valid = valid and bool(re.fullmatch(r"sha256:[0-9a-f]{64}", data.get("content_sha256", "")))
if valid:
    fields = (
        "valid",
        data["content_sha256"],
        data["distribution_version"],
        data["source_id"],
        data["installed_at"],
        data["updated_at"],
        "",
    )
    print("|".join(fields))
else:
    print("invalid||||||invalid_provenance")
PY
}

results_json='[]'
managed_count=0
customized_count=0
unmanaged_count=0
new_count=0
update_available_count=0

append_result() {
    local skill_name=$1
    local state=$2
    local action_name=$3
    local result_state=$4
    local reason=$5
    local provenance=$6
    local managed_source=$7
    local available_source=$8
    local installed_version=$9
    local available_version=${10}
    local managed_hash=${11}
    local current_hash=${12}
    local available_hash=${13}
    local installed_at=${14}
    local updated_at=${15}
    local result
    result=$(printf '{"name":%s,"state":%s,"action":%s,"result_state":%s,"reason":%s,"provenance":%s,"managed_source":%s,"available_source":%s,"installed_version":%s,"available_version":%s,"managed_hash":%s,"current_hash":%s,"available_hash":%s,"installed_at":%s,"updated_at":%s}' \
        "$(openlia_json_quote "$skill_name")" \
        "$(openlia_json_quote "$state")" \
        "$(openlia_json_quote "$action_name")" \
        "$(openlia_json_quote "$result_state")" \
        "$(openlia_json_quote "$reason")" \
        "$(openlia_json_quote "$provenance")" \
        "$(openlia_json_quote "$managed_source")" \
        "$(openlia_json_quote "$available_source")" \
        "$(openlia_json_quote "$installed_version")" \
        "$(openlia_json_quote "$available_version")" \
        "$(openlia_json_quote "$managed_hash")" \
        "$(openlia_json_quote "$current_hash")" \
        "$(openlia_json_quote "$available_hash")" \
        "$(openlia_json_quote "$installed_at")" \
        "$(openlia_json_quote "$updated_at")")
    if [[ "$results_json" == '[]' ]]; then
        results_json="[${result}]"
    else
        results_json="${results_json%]},${result}]"
    fi
}

status_skill() {
    local skill_name=$1
    local source=$2
    local destination="${OPENLIA_DATA_ROOT}/skills/${skill_name}"
    local metadata="${OPENLIA_META_ROOT}/managed/skills/${skill_name}.json"
    local source_hash='' current_hash='' metadata_info metadata_state metadata_hash metadata_version metadata_source metadata_installed metadata_updated metadata_reason result_available_source=''
    local state action_name result_state reason provenance
    local available=false
    [[ -d "$source" ]] && available=true
    if [[ "$available" != true && ! -e "$destination" ]]; then
        openlia_die "skill is not bundled or installed: ${skill_name}"
    fi
    if [[ "$available" == true ]]; then
        source_hash="sha256:$(openlia_directory_sha256 "$source")"
        result_available_source="$distribution_source"
    fi
    metadata_info=$(read_skill_metadata "$metadata" "$skill_name")
    IFS='|' read -r metadata_state metadata_hash metadata_version metadata_source metadata_installed metadata_updated metadata_reason <<<"$metadata_info"
    if [[ -e "$destination" && ! -L "$destination" && -d "$destination" ]]; then
        current_hash="sha256:$(openlia_directory_sha256 "$destination")"
    fi

    if [[ ! -e "$destination" ]]; then
        state=new
        action_name=not_installed
        result_state=new
        reason=skill_not_installed
        provenance=unavailable
        new_count=$((new_count + 1))
    elif [[ "$available" != true ]]; then
        state=unmanaged
        action_name=blocked
        result_state=unmanaged
        reason=distribution_removed
        provenance="$metadata_state"
        unmanaged_count=$((unmanaged_count + 1))
    elif [[ "$metadata_state" == valid && "$current_hash" == "$metadata_hash" ]]; then
        state=managed
        result_state=managed
        provenance=valid
        if [[ "$current_hash" == "$source_hash" ]]; then
            action_name=unchanged
            reason=managed_artifact_unchanged
            managed_count=$((managed_count + 1))
        else
            action_name=update_available
            reason=distribution_changed
            managed_count=$((managed_count + 1))
            update_available_count=$((update_available_count + 1))
        fi
    elif [[ "$metadata_state" == valid ]]; then
        state=customized
        action_name=blocked
        result_state=customized
        reason=local_modifications_detected
        provenance=valid
        customized_count=$((customized_count + 1))
    else
        state=unmanaged
        action_name=blocked
        result_state=unmanaged
        reason="$metadata_reason"
        provenance="$metadata_state"
        unmanaged_count=$((unmanaged_count + 1))
    fi
    append_result "$skill_name" "$state" "$action_name" "$result_state" "$reason" "$provenance" "$metadata_source" "$result_available_source" "$metadata_version" "$distribution_version" "$metadata_hash" "$current_hash" "$source_hash" "$metadata_installed" "$metadata_updated"
}

if [[ -n "$selected_skill" ]]; then
    source_directory="${OPENLIA_REPO_ROOT}/profile/skills/${selected_skill}"
    if [[ -d "$source_directory" ]]; then
        status_skill "$selected_skill" "$source_directory"
    else
        status_skill "$selected_skill" ''
    fi
else
    for skill_directory in "${OPENLIA_REPO_ROOT}/profile/skills"/*; do
        [[ -d "$skill_directory" ]] || continue
        skill=${skill_directory##*/}
        openlia_require_safe_component "$skill" skill
        status_skill "$skill" "$skill_directory"
    done
    for runtime_skill in "${OPENLIA_DATA_ROOT}/skills"/*; do
        [[ -d "$runtime_skill" || -L "$runtime_skill" ]] || continue
        skill=${runtime_skill##*/}
        [[ "$skill" == .openlia-disabled ]] && continue
        [[ -d "${OPENLIA_REPO_ROOT}/profile/skills/${skill}" ]] && continue
        status_skill "$skill" ''
    done
fi

if [[ "$json" == true ]]; then
    printf '{"schema":1,"ok":true,"action":"profile-status","workspace":"preserved","selected_skill":%s,"summary":{"managed":%d,"customized":%d,"unmanaged":%d,"new":%d,"update_available":%d},"skills":{"results":%s}}\n' \
        "$(openlia_json_quote "$selected_skill")" "$managed_count" "$customized_count" "$unmanaged_count" "$new_count" "$update_available_count" "$results_json"
else
    python3 - "$results_json" <<'PY'
import json
import sys

for item in json.loads(sys.argv[1]):
    print(f"{item['state']:<11} {item['name']}")
    if item["action"] == "update_available":
        print(f"  update available: {item['installed_version']} -> {item['available_version']}")
    elif item["action"] == "blocked":
        print(f"  automatic update blocked: {item['reason']}")
    elif item["action"] == "not_installed":
        print("  not installed in the runtime")
    else:
        print("  no update needed")
PY
fi
