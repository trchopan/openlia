#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

action=sync
json=false
if (($#)) && [[ "$1" != -* ]]; then
    action=$1
    shift
fi
while (($#)); do
    case "$1" in
        --json) json=true ;;
        -h|--help)
            printf '%s\n' 'Usage: profile.sh sync [--json]'
            exit 0
            ;;
        *) printf 'openlia: unknown profile option %s\n' "$1" >&2; exit 2 ;;
    esac
    shift
done
[[ "$action" == sync ]] || { printf 'openlia: unknown profile action %s\n' "$action" >&2; exit 2; }
openlia_validate_paths
openlia_require_safe_component "$OPENLIA_PROJECT_NAME" project-name
openlia_require_command python3
openlia_ensure_dir "$OPENLIA_DATA_ROOT" 700
openlia_ensure_dir "${OPENLIA_DATA_ROOT}/skills" 700
openlia_ensure_dir "${OPENLIA_DATA_ROOT}/scripts" 700
openlia_ensure_dir "${OPENLIA_META_ROOT}/managed" 700
openlia_ensure_dir "${OPENLIA_META_ROOT}/managed/skills" 700

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

sync_file() {
    local source=$1
    local destination=$2
    local marker=$3
    local mode=${4:-644}
    local source_hash destination_hash previous_hash
    [[ -f "$source" ]] || return 0
    mkdir -p "$(dirname "$destination")"
    source_hash=$(openlia_sha256 "$source" | awk '{print $1}')
    if [[ ! -e "$destination" ]]; then
        openlia_atomic_copy "$source" "$destination" "$mode"
        printf '%s\n' "$source_hash" >"$marker"
        chmod 600 "$marker"
        chmod "$mode" "$destination"
        return
    fi
    previous_hash=''
    [[ -f "$marker" ]] && previous_hash=$(<"$marker")
    destination_hash=$(openlia_sha256 "$destination" | awk '{print $1}')
    if [[ -z "$previous_hash" && "$destination_hash" == "$source_hash" ]]; then
        printf '%s\n' "$source_hash" >"$marker"
        chmod 600 "$marker"
        chmod "$mode" "$destination"
    elif [[ -n "$previous_hash" && "$destination_hash" == "$previous_hash" ]]; then
        openlia_atomic_copy "$source" "$destination" "$mode"
        printf '%s\n' "$source_hash" >"$marker"
        chmod 600 "$marker"
        chmod "$mode" "$destination"
    fi
}

sync_file "${OPENLIA_REPO_ROOT}/profile/SOUL.md" "${OPENLIA_DATA_ROOT}/SOUL.md" "${OPENLIA_META_ROOT}/managed/SOUL.md.sha256"
sync_file "${OPENLIA_REPO_ROOT}/profile/AGENTS.md" "${OPENLIA_DATA_ROOT}/AGENTS.md" "${OPENLIA_META_ROOT}/managed/AGENTS.md.sha256"
sync_file "${OPENLIA_REPO_ROOT}/profile/config.yaml" "${OPENLIA_DATA_ROOT}/config.yaml" "${OPENLIA_META_ROOT}/managed/config.yaml.sha256"
sync_file "${OPENLIA_REPO_ROOT}/profile/cron/scripts/openlia-workspace-git-sync.sh" "${OPENLIA_DATA_ROOT}/scripts/openlia-workspace-git-sync.sh" "${OPENLIA_META_ROOT}/managed/openlia-workspace-git-sync.sh.sha256" 700

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
reason = "invalid_provenance"
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
    print("invalid||||||" + reason)
PY
}

write_skill_metadata() {
    local metadata=$1
    local skill_name=$2
    local content_hash=$3
    local installed_at=${4:-}
    local updated_at
    [[ -n "$installed_at" ]] || installed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    updated_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    skill_metadata_written_installed_at=$installed_at
    skill_metadata_written_updated_at=$updated_at
    openlia_atomic_stdin "$metadata" 600 <<EOF
{"schema":1,"skill":$(openlia_json_quote "$skill_name"),"distribution":$(openlia_json_quote 'openlia-personal-os'),"distribution_version":$(openlia_json_quote "$distribution_version"),"source_id":$(openlia_json_quote "$distribution_source"),"artifact_path":$(openlia_json_quote "profile/skills/${skill_name}"),"hash_scope":"skill-files-v1","content_sha256":$(openlia_json_quote "$content_hash"),"installed_at":$(openlia_json_quote "$installed_at"),"updated_at":$(openlia_json_quote "$updated_at")}
EOF
}

skill_metadata_written_installed_at=''
skill_metadata_written_updated_at=''

skill_results_json='[]'
installed_skills=0
updated_skills=0
unchanged_skills=0
customized_skills=0
unmanaged_skills=0
customized_skill_names=()
unmanaged_skill_names=()
blocked_human=''

append_skill_result() {
    local skill_name=$1
    local state=$2
    local action_name=$3
    local result_state=$4
    local reason=$5
    local provenance=$6
    local managed_source=$7
    local installed_version=$8
    local available_version=$9
    local managed_hash=${10}
    local current_hash=${11}
    local available_hash=${12}
    local installed_at=${13}
    local updated_at=${14}
    local result
    result=$(printf '{"name":%s,"state":%s,"action":%s,"result_state":%s,"reason":%s,"provenance":%s,"managed_source":%s,"installed_version":%s,"available_version":%s,"managed_hash":%s,"current_hash":%s,"available_hash":%s,"installed_at":%s,"updated_at":%s}' \
        "$(openlia_json_quote "$skill_name")" \
        "$(openlia_json_quote "$state")" \
        "$(openlia_json_quote "$action_name")" \
        "$(openlia_json_quote "$result_state")" \
        "$(openlia_json_quote "$reason")" \
        "$(openlia_json_quote "$provenance")" \
        "$(openlia_json_quote "$managed_source")" \
        "$(openlia_json_quote "$installed_version")" \
        "$(openlia_json_quote "$available_version")" \
        "$(openlia_json_quote "$managed_hash")" \
        "$(openlia_json_quote "$current_hash")" \
        "$(openlia_json_quote "$available_hash")" \
        "$(openlia_json_quote "$installed_at")" \
        "$(openlia_json_quote "$updated_at")")
    if [[ "$skill_results_json" == '[]' ]]; then
        skill_results_json="[${result}]"
    else
        skill_results_json="${skill_results_json%]},${result}]"
    fi
}

record_blocked_skill() {
    local state=$1
    local skill_name=$2
    local reason=$3
    blocked_human+="${state} ${skill_name}\n  automatic update blocked: ${reason}\n"
}

sync_skill() {
    local source=$1
    local destination=$2
    local metadata=$3
    local skill_name=$4
    local source_hash current_hash metadata_info metadata_state metadata_hash metadata_version metadata_source metadata_installed metadata_updated metadata_reason
    local state action_name result_state reason provenance
    source_hash="sha256:$(openlia_directory_sha256 "$source")"
    current_hash=''
    metadata_info=$(read_skill_metadata "$metadata" "$skill_name")
    IFS='|' read -r metadata_state metadata_hash metadata_version metadata_source metadata_installed metadata_updated metadata_reason <<<"$metadata_info"
    if [[ ! -e "$destination" ]]; then
        cp -a "$source" "$destination"
        write_skill_metadata "$metadata" "$skill_name" "$source_hash"
        metadata_hash="$source_hash"
        metadata_version="$distribution_version"
        metadata_source="$distribution_source"
        metadata_installed="$skill_metadata_written_installed_at"
        metadata_updated="$skill_metadata_written_updated_at"
        current_hash="$source_hash"
        state=new
        action_name=installed
        result_state=managed
        reason=installed_from_distribution
        provenance=created
        installed_skills=$((installed_skills + 1))
    elif [[ -L "$destination" || ! -d "$destination" ]]; then
        if [[ "$metadata_state" == valid ]]; then
            state=customized
            provenance=valid
            reason=runtime_skill_shape_changed
        else
            state=unmanaged
            provenance="$metadata_state"
            reason="$metadata_reason"
        fi
        action_name=blocked
        result_state="$state"
        if [[ "$state" == customized ]]; then
            customized_skills=$((customized_skills + 1))
            customized_skill_names+=("$skill_name")
        else
            unmanaged_skills=$((unmanaged_skills + 1))
            unmanaged_skill_names+=("$skill_name")
        fi
        record_blocked_skill "$state" "$skill_name" "$reason"
    else
        current_hash="sha256:$(openlia_directory_sha256 "$destination")"
        if [[ "$metadata_state" == valid && "$current_hash" == "$metadata_hash" ]]; then
            provenance=valid
            if [[ "$current_hash" == "$source_hash" ]]; then
                state=managed
                action_name=unchanged
                result_state=managed
                reason=managed_artifact_unchanged
                unchanged_skills=$((unchanged_skills + 1))
            else
                openlia_atomic_copy_dir "$source" "$destination"
                write_skill_metadata "$metadata" "$skill_name" "$source_hash" "$metadata_installed"
                metadata_hash="$source_hash"
                metadata_version="$distribution_version"
                metadata_source="$distribution_source"
                metadata_updated="$skill_metadata_written_updated_at"
                state=managed
                action_name=updated
                result_state=managed
                reason=managed_artifact_updated
                current_hash="$source_hash"
                updated_skills=$((updated_skills + 1))
            fi
            find "$destination" -type d -exec chmod 755 {} +
            find "$destination" -type f -exec chmod 644 {} +
        elif [[ "$metadata_state" == valid ]]; then
            state=customized
            action_name=blocked
            result_state=customized
            provenance=valid
            reason=local_modifications_detected
            customized_skills=$((customized_skills + 1))
            customized_skill_names+=("$skill_name")
            record_blocked_skill "$state" "$skill_name" "$reason"
        else
            state=unmanaged
            action_name=blocked
            result_state=unmanaged
            provenance="$metadata_state"
            reason="$metadata_reason"
            unmanaged_skills=$((unmanaged_skills + 1))
            unmanaged_skill_names+=("$skill_name")
            record_blocked_skill "$state" "$skill_name" "$reason"
        fi
    fi
    append_skill_result "$skill_name" "$state" "$action_name" "$result_state" "$reason" "$provenance" "$metadata_source" "$metadata_version" "$distribution_version" "$metadata_hash" "$current_hash" "$source_hash" "$metadata_installed" "$metadata_updated"
}

report_removed_skill() {
    local destination=$1
    local skill_name=$2
    local metadata="${OPENLIA_META_ROOT}/managed/skills/${skill_name}.json"
    local metadata_info metadata_state metadata_hash metadata_version metadata_source metadata_installed metadata_updated metadata_reason current_hash
    metadata_info=$(read_skill_metadata "$metadata" "$skill_name")
    IFS='|' read -r metadata_state metadata_hash metadata_version metadata_source metadata_installed metadata_updated metadata_reason <<<"$metadata_info"
    current_hash=''
    if [[ -d "$destination" && ! -L "$destination" ]]; then
        current_hash="sha256:$(openlia_directory_sha256 "$destination")"
    fi
    unmanaged_skills=$((unmanaged_skills + 1))
    unmanaged_skill_names+=("$skill_name")
    record_blocked_skill unmanaged "$skill_name" distribution_removed
    append_skill_result "$skill_name" unmanaged blocked unmanaged distribution_removed "$metadata_state" "$metadata_source" "$metadata_version" '' "$metadata_hash" "$current_hash" '' "$metadata_installed" "$metadata_updated"
}

enabled_skills=()
if [[ -n "${OPENLIA_ENABLED_SKILLS:-}" ]]; then
    IFS=',' read -r -a enabled_skills <<<"$OPENLIA_ENABLED_SKILLS"
fi
skills_configured=${OPENLIA_SKILLS_CONFIGURED:-false}
for skill_directory in "${OPENLIA_REPO_ROOT}/profile/skills"/*; do
    [[ -d "$skill_directory" ]] || continue
    skill=${skill_directory##*/}
    openlia_require_safe_component "$skill" skill
    enabled=false
    if [[ "$skills_configured" != true && -z "${OPENLIA_ENABLED_SKILLS:-}" ]]; then
        enabled=true
    fi
    if ((${#enabled_skills[@]} > 0)); then
        for selected in "${enabled_skills[@]}"; do
            [[ "$selected" == "$skill" ]] && enabled=true
        done
    fi
    destination="${OPENLIA_DATA_ROOT}/skills/${skill}"
    disabled_destination="${OPENLIA_DATA_ROOT}/skills/.openlia-disabled/${skill}"
    if [[ "$enabled" == true ]]; then
        if [[ -d "$disabled_destination" && ! -e "$destination" ]]; then
            mv "$disabled_destination" "$destination"
        fi
        sync_skill "$skill_directory" "$destination" "${OPENLIA_META_ROOT}/managed/skills/${skill}.json" "$skill"
    elif [[ -d "$destination" ]]; then
        openlia_ensure_dir "${OPENLIA_DATA_ROOT}/skills/.openlia-disabled" 700
        mv "$destination" "$disabled_destination"
    fi
done

if [[ -d "${OPENLIA_DATA_ROOT}/skills" ]]; then
    for runtime_skill in "${OPENLIA_DATA_ROOT}/skills"/*; do
        [[ -d "$runtime_skill" || -L "$runtime_skill" ]] || continue
        skill=${runtime_skill##*/}
        [[ "$skill" == .openlia-disabled ]] && continue
        [[ -d "${OPENLIA_REPO_ROOT}/profile/skills/${skill}" ]] && continue
        report_removed_skill "$runtime_skill" "$skill"
    done
fi

customized_json='[]'
if ((${#customized_skill_names[@]} > 0)); then
    for skill in "${customized_skill_names[@]}"; do
        skill_json=$(openlia_json_quote "$skill")
        if [[ "$customized_json" == '[]' ]]; then
            customized_json="[${skill_json}]"
        else
            customized_json="${customized_json%]},${skill_json}]"
        fi
    done
fi
unmanaged_json='[]'
if ((${#unmanaged_skill_names[@]} > 0)); then
    for skill in "${unmanaged_skill_names[@]}"; do
        skill_json=$(openlia_json_quote "$skill")
        if [[ "$unmanaged_json" == '[]' ]]; then
            unmanaged_json="[${skill_json}]"
        else
            unmanaged_json="${unmanaged_json%]},${skill_json}]"
        fi
    done
fi
detail="profile assets synchronized without workspace replacement; updated=${updated_skills}; customized=${customized_skills}; unmanaged=${unmanaged_skills}"
openlia_record_change profile-sync ok '' "$detail"
if [[ "$json" == true ]]; then
    printf '{"ok":true,"action":"profile-sync","workspace":"preserved","skills":{"installed":%d,"updated":%d,"unchanged":%d,"customized":%d,"unmanaged":%d,"customized_names":%s,"unmanaged_names":%s,"results":%s}}\n' \
        "$installed_skills" "$updated_skills" "$unchanged_skills" "$customized_skills" "$unmanaged_skills" "$customized_json" "$unmanaged_json" "$skill_results_json"
else
    if [[ -n "$blocked_human" ]]; then
        printf '%b' "$blocked_human"
    fi
    printf 'openlia profile: distribution assets synchronized; workspace preserved; skills updated=%d customized=%d unmanaged=%d\n' \
        "$updated_skills" "$customized_skills" "$unmanaged_skills"
fi
