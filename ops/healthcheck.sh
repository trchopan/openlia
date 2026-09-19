#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

json=false
allow_stopped=false
provider_check=false

usage() {
    printf '%s\n' 'Usage: healthcheck.sh [--json] [--allow-stopped] [--provider-check]'
}

while (($#)); do
    case "$1" in
        --json) json=true ;;
        --allow-stopped) allow_stopped=true ;;
        --provider-check) provider_check=true ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
    shift
done

openlia_validate_paths
openlia_require_command python3
openlia_require_command docker
openlia_require_command tar
openlia_sha256 /dev/null >/dev/null

checks=''
failed=0

add_check() {
    local name=$1
    local ok=$2
    local detail=$3
    local item
    item=$(printf '{"name":%s,"ok":%s,"detail":%s}' \
        "$(openlia_json_quote "$name")" \
        "$(openlia_json_bool "$ok")" \
        "$(openlia_json_quote "$detail")")
    [[ -z "$checks" ]] || checks+=,
    checks+=$item
    [[ "$ok" == true ]] || failed=1
}

if docker info >/dev/null 2>&1; then
    add_check docker true available
else
    add_check docker false unavailable
fi

if command -v findmnt >/dev/null 2>&1; then
    filesystem=$(findmnt -T "$OPENLIA_DATA_ROOT" -n -o FSTYPE 2>/dev/null || true)
    case "$filesystem" in
        nfs|nfs4|cifs|smbfs|fuse.*) add_check data_filesystem false "unsupported:${filesystem}" ;;
        '') add_check data_filesystem false unavailable ;;
        *) add_check data_filesystem true "$filesystem" ;;
    esac
else
    filesystem=$(stat -f '%T' "$OPENLIA_DATA_ROOT" 2>/dev/null || true)
    case "$filesystem" in
        nfs|smbfs|cifs|fuse.*) add_check data_filesystem false "unsupported:${filesystem}" ;;
        '') add_check data_filesystem true unverified ;;
        *) add_check data_filesystem true "$filesystem" ;;
    esac
fi

if [[ -f "$OPENLIA_COMPOSE_FILE" ]] && openlia_compose config --quiet >/dev/null 2>&1; then
    add_check compose true valid
else
    add_check compose false invalid_or_missing
fi

state=$(openlia_read_state)
if [[ "$state" == running ]]; then
    add_check explicit_stop_state true running
elif [[ "$state" == stopped ]]; then
    add_check explicit_stop_state true stopped
    if [[ "$allow_stopped" != true ]]; then
        add_check expected_running false explicitly_stopped
    fi
else
    add_check explicit_stop_state true never_started
    add_check expected_running false never_started
fi

services=''
if services=$(openlia_compose config --services 2>/dev/null); then
    while IFS= read -r service || [[ -n "$service" ]]; do
        [[ -n "$service" ]] || continue
        if openlia_service_running "$service"; then
            add_check "service:${service}" true running
            if [[ "$service" == locho-* ]]; then
                published=$(openlia_compose port "$service" 2>/dev/null || true)
                if [[ -z "$published" ]]; then
                    add_check "listener:${service}" true private_only
                else
                    add_check "listener:${service}" false published
                fi
            fi
        elif [[ "$state" == stopped && "$allow_stopped" == true ]]; then
            add_check "service:${service}" true stopped
        else
            add_check "service:${service}" false not_running
        fi
    done <<<"$services"
else
    add_check services false unavailable
fi

if openlia_service_running hermes; then
    if openlia_compose exec -T hermes sh -c 'command -v hermes >/dev/null && command -v bun >/dev/null && command -v uv >/dev/null && command -v git >/dev/null' >/dev/null 2>&1; then
        add_check hermes_runtimes true hermes_bun_uv_git_available
    else
        add_check hermes_runtimes false hermes_bun_uv_git_missing
    fi
    if openlia_compose exec -T hermes hermes config check >/dev/null 2>&1; then
        add_check hermes_config true valid
    else
        add_check hermes_config false invalid
    fi
    if [[ "$provider_check" != true ]]; then
        add_check hermes_doctor true not_requested
    elif openlia_compose exec -T hermes hermes doctor >/dev/null 2>&1; then
        add_check hermes_doctor true healthy
    else
        add_check hermes_doctor false failed
    fi
    if openlia_compose exec -T hermes sh -c 'test -d /opt/data/workspace && test -f /opt/data/workspace/AGENTS.md' >/dev/null 2>&1; then
        add_check workspace true initialized
    else
        add_check workspace false missing_or_uninitialized
    fi
    if [[ "$provider_check" == true ]]; then
        if openlia_compose exec -T hermes hermes chat --oneshot -q 'Reply with OPENLIA_PROVIDER_CHECK' >/dev/null 2>&1; then
            add_check provider_request true completed
        else
            add_check provider_request false failed_or_unconfigured
        fi
    else
        add_check provider_request true not_requested
    fi
else
    if [[ "$state" == stopped && "$allow_stopped" == true ]]; then
        add_check hermes_runtimes true stopped
        add_check hermes_config true stopped
        add_check hermes_doctor true not_requested
    else
        add_check hermes_runtimes false hermes_not_running
        add_check hermes_config false hermes_not_running
        add_check hermes_doctor false hermes_not_running
    fi
    if openlia_compose exec -T hermes sh -c 'test -d /opt/data/workspace && test -f /opt/data/workspace/AGENTS.md' >/dev/null 2>&1; then
        add_check workspace true initialized
    elif [[ "$state" == stopped && "$allow_stopped" == true ]]; then
        add_check workspace true stopped
    else
        add_check workspace false hermes_not_running
    fi
    add_check provider_request true not_requested
fi

container_id=$(openlia_compose ps -q hermes 2>/dev/null || true)
if [[ -n "$container_id" ]]; then
    privileged=$(docker inspect "$container_id" --format '{{.HostConfig.Privileged}}' 2>/dev/null || printf unknown)
    if [[ "$privileged" == false ]]; then
        add_check container_privileged true disabled
    else
        add_check container_privileged false "$privileged"
    fi
    mount_list=$(docker inspect "$container_id" --format '{{range .Mounts}}{{.Source}} {{.Destination}}\n{{end}}' 2>/dev/null || true)
    if [[ "$mount_list" == *docker.sock* ]]; then
        add_check docker_socket false mounted
    else
        add_check docker_socket true absent
    fi
else
    if [[ "$state" == stopped && "$allow_stopped" == true ]]; then
        add_check container_privileged true stopped
        add_check docker_socket true stopped
    else
        add_check container_privileged false hermes_container_missing
        add_check docker_socket false hermes_container_missing
    fi
fi

if [[ -f "$OPENLIA_SECRET_FILE" ]]; then
    if [[ "$(openlia_file_mode "$OPENLIA_SECRET_FILE")" == 600 ]]; then
        add_check secret_source true mode_0600
    else
        add_check secret_source false mode_not_0600
    fi
else
    add_check secret_source false missing
fi

if [[ "$json" == true ]]; then
    printf '{"schema":1,"ok":%s,"state":%s,"checks":[%s],"secrets":"redacted","capabilities":"redacted"}\n' \
        "$(openlia_json_bool "$([[ "$failed" -eq 0 ]] && printf true || printf false)")" \
        "$(openlia_json_quote "$state")" "$checks"
else
    printf 'openlia health: state=%s result=%s\n' "$state" "$([[ "$failed" -eq 0 ]] && printf ok || printf failed)"
    while IFS= read -r service || [[ -n "$service" ]]; do
        [[ -n "$service" ]] && printf '  service=%s\n' "$service"
    done <<<"$services"
fi

if ((failed != 0)); then
    exit 1
fi
