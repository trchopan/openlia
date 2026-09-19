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
openlia_ensure_dir "$OPENLIA_DATA_ROOT" 700
openlia_ensure_dir "${OPENLIA_DATA_ROOT}/skills" 700
openlia_ensure_dir "${OPENLIA_DATA_ROOT}/scripts" 700
openlia_ensure_dir "${OPENLIA_META_ROOT}/managed" 700

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
        if [[ ! -e "$destination" ]]; then
            cp -a "$skill_directory" "$destination"
        fi
        find "$destination" -type d -exec chmod 755 {} +
        find "$destination" -type f -exec chmod 644 {} +
    elif [[ -d "$destination" ]]; then
        openlia_ensure_dir "${OPENLIA_DATA_ROOT}/skills/.openlia-disabled" 700
        mv "$destination" "$disabled_destination"
    fi
done

openlia_record_change profile-sync ok '' 'profile assets synchronized without workspace replacement'
if [[ "$json" == true ]]; then
    printf '%s\n' '{"ok":true,"action":"profile-sync","workspace":"preserved","skills":"synchronized"}'
else
    printf '%s\n' 'openlia profile: distribution assets synchronized; workspace preserved'
fi
