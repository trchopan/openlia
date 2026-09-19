#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

json=false
check_only=false

usage() {
    printf '%s\n' \
        'Usage: bootstrap.sh [--json] [--check-only]' \
        '  Prepare runtime directories and the empty Docker secret source.'
}

while (($#)); do
    case "$1" in
        --json) json=true ;;
        --check-only) check_only=true ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
    shift
done

openlia_validate_paths
openlia_require_command python3
openlia_require_command docker
openlia_require_command tar
openlia_require_command sha256sum

if [[ "$(uname -s)" != Linux ]]; then
    openlia_die 'the deployment target must be Linux'
fi

if ! docker compose version >/dev/null 2>&1; then
    openlia_die 'Docker Compose v2 is required'
fi

if [[ "$check_only" == true ]]; then
    if [[ -d "$OPENLIA_RUNTIME_ROOT" ]]; then
        printf '%s\n' '{"ok":true,"action":"bootstrap-check","runtime_root":"configured"}'
    else
        printf '%s\n' '{"ok":false,"action":"bootstrap-check","runtime_root":"missing"}'
        exit 1
    fi
    exit 0
fi

openlia_ensure_dir "$OPENLIA_RUNTIME_ROOT" 700
openlia_ensure_dir "$OPENLIA_DATA_ROOT" 700
openlia_ensure_dir "$OPENLIA_LOCHO_ROOT" 700
openlia_ensure_dir "${OPENLIA_RUNTIME_ROOT}/secrets" 700
chown 10000:10000 "${OPENLIA_RUNTIME_ROOT}/secrets"
openlia_ensure_dir "$OPENLIA_BACKUP_ROOT" 700
openlia_ensure_dir "$OPENLIA_META_ROOT" 700

if [[ ! -e "$OPENLIA_SECRET_FILE" ]]; then
    openlia_atomic_stdin "$OPENLIA_SECRET_FILE" 600 <<'EOF'
# Add KEY=VALUE lines through the operator's secret rotation workflow.
EOF
else
    [[ -f "$OPENLIA_SECRET_FILE" ]] || openlia_die 'secret source path is not a regular file'
    chmod 600 "$OPENLIA_SECRET_FILE"
fi
chown 10000:10000 "$OPENLIA_SECRET_FILE"
chmod 600 "$OPENLIA_SECRET_FILE"

# This is only Hermes orchestration configuration. It contains no provider
# values and is created once so a deployment can use secrets.command without
# overwriting an operator's later config changes.
if [[ ! -e "${OPENLIA_DATA_ROOT}/config.yaml" ]]; then
    if [[ -f "${OPENLIA_REPO_ROOT}/profile/config.yaml" ]]; then
        cp -- "${OPENLIA_REPO_ROOT}/profile/config.yaml" "${OPENLIA_DATA_ROOT}/config.yaml"
        chmod 600 "${OPENLIA_DATA_ROOT}/config.yaml"
    else
        openlia_atomic_stdin "${OPENLIA_DATA_ROOT}/config.yaml" 600 <<'EOF'
secrets:
  command:
    enabled: true
    command: /usr/local/bin/openlia-secret-source
    helper_timeout_seconds: 3
    override_existing: true
EOF
    fi
fi

# Profile-owned files are seeded once. Workspace files are never overwritten:
# an empty directory is the only condition under which the template is copied.
for profile_file in SOUL.md AGENTS.md; do
    if [[ ! -e "${OPENLIA_DATA_ROOT}/${profile_file}" && -f "${OPENLIA_REPO_ROOT}/profile/${profile_file}" ]]; then
        cp -- "${OPENLIA_REPO_ROOT}/profile/${profile_file}" "${OPENLIA_DATA_ROOT}/${profile_file}"
        chmod 600 "${OPENLIA_DATA_ROOT}/${profile_file}"
    fi
done
workspace_dir="${OPENLIA_DATA_ROOT}/workspace"
mkdir -p -- "$workspace_dir"
if [[ -z "$(find "$workspace_dir" -mindepth 1 -maxdepth 1 -print -quit)" && -d "${OPENLIA_REPO_ROOT}/workspace-template" ]]; then
    cp -a -- "${OPENLIA_REPO_ROOT}/workspace-template/." "$workspace_dir/"
    find "$workspace_dir" -type d -exec chmod 700 {} +
    find "$workspace_dir" -type f -exec chmod 600 {} +
fi
# Adding missing category directories is non-destructive and repairs releases
# created before marker files were included in the embedded archive.
for workspace_category in inbox goals areas projects knowledge ideas decisions monitors tasks calendar people shopping travel finance archive; do
    mkdir -p -- "${workspace_dir}/${workspace_category}"
done

if [[ -x "${SCRIPT_DIR}/profile.sh" ]]; then
    "${SCRIPT_DIR}/profile.sh" sync --json >/dev/null
fi
if [[ -x "${SCRIPT_DIR}/attachments.sh" ]]; then
    "${SCRIPT_DIR}/attachments.sh" generate --json >/dev/null
fi

if [[ ! -e "$OPENLIA_STATE_FILE" ]]; then
    openlia_write_state never-started
fi

openlia_atomic_stdin "${OPENLIA_META_ROOT}/runtime.json" 600 <<EOF
{"schema":1,"runtime_root":$(openlia_json_quote "$OPENLIA_RUNTIME_ROOT"),"hermes_data":$(openlia_json_quote "$OPENLIA_DATA_ROOT"),"locho_root":$(openlia_json_quote "$OPENLIA_LOCHO_ROOT"),"secret_source":"docker-secret","created_at":$(openlia_json_quote "$(date -u +%Y-%m-%dT%H:%M:%SZ)")}
EOF

openlia_record_change bootstrap ok '' 'runtime initialized'
if [[ "$json" == true ]]; then
    printf '%s\n' '{"ok":true,"action":"bootstrap","state":"never-started","secret_values":"not_reported"}'
else
    printf '%s\n' 'openlia bootstrap: runtime prepared; no services started'
fi
