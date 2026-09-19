#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

action=deploy
force_start=false
json=false
component=all

usage() {
    printf '%s\n' \
        'Usage: deploy.sh [deploy|start|stop|restart|profile] [--component all|hermes|locho] [--start] [--json]' \
        '  deploy validates and reconciles the stack; it honors an explicit stop.'
}

if (($#)) && [[ "$1" != -* ]]; then
    action=$1
    shift
fi
while (($#)); do
    case "$1" in
        --start) force_start=true ;;
        --component)
            (($# >= 2)) || { usage >&2; exit 2; }
            component=$2
            shift
            ;;
        --json) json=true ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
    shift
done

case "$action" in
    deploy|start|stop|restart|profile) ;;
    *) usage >&2; exit 2 ;;
esac
case "$component" in
    all|hermes|locho) ;;
    *) usage >&2; exit 2 ;;
esac

openlia_validate_paths
openlia_require_command python3
openlia_require_command docker
openlia_require_command tar
openlia_sha256 /dev/null >/dev/null
[[ -f "$OPENLIA_COMPOSE_FILE" ]] || openlia_die 'base Compose file is missing'
[[ -f "$OPENLIA_SECRET_FILE" ]] || openlia_die 'Docker secret source is missing; run bootstrap first'

if [[ "$action" == profile ]]; then
    backup_output=$(
        "${SCRIPT_DIR}/backup.sh" create --reason profile-update --json
    )
    backup_path=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["archive"])' <<<"$backup_output")
    if ! "${SCRIPT_DIR}/profile.sh" sync --json >/dev/null; then
        openlia_record_change profile failed "$backup_path" 'profile synchronization failed'
        openlia_die 'profile synchronization failed; workspace was not replaced'
    fi
    openlia_record_change profile ok "$backup_path" 'profile assets synchronized'
    if [[ "$json" == true ]]; then
        printf '{"ok":true,"action":"profile","backup":%s,"workspace":"preserved"}\n' "$(openlia_json_quote "$backup_path")"
    else
        printf 'openlia deploy: profile assets synchronized; workspace preserved\n'
    fi
    exit 0
fi

case "$action" in
    stop)
        openlia_ensure_dir "$OPENLIA_META_ROOT" 700
        previous_state=$(openlia_read_state)
        if ! openlia_compose stop >/dev/null 2>&1; then
            openlia_write_state "$previous_state"
            openlia_record_change stop failed '' 'Compose stop failed'
            openlia_die 'Compose stop failed'
        fi
        openlia_write_state stopped
        openlia_record_change stop ok '' 'explicit stop recorded'
        if [[ "$json" == true ]]; then
            printf '%s\n' '{"ok":true,"action":"stop","state":"stopped"}'
        else
            printf '%s\n' 'openlia deploy: stack stopped'
        fi
        exit 0
        ;;
esac

previous_state=$(openlia_read_state)
if [[ "$action" == start || "$action" == restart ]]; then
    force_start=true
fi

if [[ "$action" == deploy || "$action" == restart ]]; then
    # The backup is made before image/config/container mutation. The archive
    # deliberately excludes secret and capability files.
    backup_output=$(
        "${SCRIPT_DIR}/backup.sh" create --reason "$action" --json
    )
    backup_path=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["archive"])' <<<"$backup_output")
else
    backup_path=''
fi

if ! openlia_compose config --quiet >/dev/null 2>&1; then
    openlia_record_change "$action" failed "$backup_path" 'Compose configuration validation failed'
    openlia_die 'Compose configuration validation failed'
fi

if [[ "$action" == deploy ]]; then
	if [[ "$component" == all || "$component" == hermes ]]; then
		if ! openlia_compose build hermes >/dev/null 2>&1; then
			openlia_record_change "$action" failed "$backup_path" 'Hermes image build failed'
			openlia_die 'Hermes image build failed; previous state was not removed'
		fi
	fi
	if [[ "$component" == all || "$component" == locho ]]; then
		locho_services=()
		while IFS= read -r service || [[ -n "$service" ]]; do
			[[ "$service" == locho-* ]] && locho_services+=("$service")
		done < <(openlia_compose config --services 2>/dev/null || true)
		if ((${#locho_services[@]} > 0)) && ! openlia_compose build "${locho_services[@]}" >/dev/null 2>&1; then
			openlia_record_change "$action" failed "$backup_path" 'Locho image build failed'
			openlia_die 'Locho image build failed; previous state was not removed'
		fi
	fi
fi

should_start=false
if [[ "$force_start" == true ]]; then
    should_start=true
elif [[ "$previous_state" == never-started ]]; then
    should_start=true
fi

if [[ "$should_start" == true ]]; then
	start_failed=false
	if [[ "$action" == restart ]]; then
		openlia_compose restart >/dev/null 2>&1 || start_failed=true
	elif [[ "$component" == all ]]; then
		if [[ "$action" == deploy ]]; then
			openlia_compose up -d --build >/dev/null 2>&1 || start_failed=true
		else
			openlia_compose up -d >/dev/null 2>&1 || start_failed=true
		fi
	elif [[ "$component" == hermes ]]; then
		if [[ "$action" == deploy ]]; then
			openlia_compose up -d --build --no-deps hermes >/dev/null 2>&1 || start_failed=true
		else
			openlia_compose up -d --no-deps hermes >/dev/null 2>&1 || start_failed=true
		fi
	else
		locho_services=()
		while IFS= read -r service || [[ -n "$service" ]]; do
			[[ "$service" == locho-* ]] && locho_services+=("$service")
		done < <(openlia_compose config --services 2>/dev/null || true)
		if ((${#locho_services[@]} > 0)); then
			if [[ "$action" == deploy ]]; then
				openlia_compose up -d --build --no-deps "${locho_services[@]}" >/dev/null 2>&1 || start_failed=true
			else
				openlia_compose up -d --no-deps "${locho_services[@]}" >/dev/null 2>&1 || start_failed=true
			fi
		fi
	fi
	if [[ "$start_failed" == true ]]; then
		openlia_record_change "$action" failed "$backup_path" 'Compose start failed'
        openlia_die 'Compose start failed; previous state was not removed'
	fi
	# The SSH operator seeds distribution files before Docker starts. Normalize
	# the user-owned workspace after the container is available so Hermes uid
	# 10000 can traverse it without making the host operator read personal data.
	if openlia_service_running hermes; then
		openlia_compose exec -T -u root hermes sh -c 'chown -R 10000:10000 /opt/data/workspace && chmod 700 /opt/data/workspace' >/dev/null 2>&1 || true
	fi
	openlia_write_state running
    healthy=false
    for attempt in {1..30}; do
        : "$attempt"
        if "${SCRIPT_DIR}/healthcheck.sh" --json >/dev/null 2>&1; then
            healthy=true
            break
        fi
        sleep 2
    done
    if [[ "$healthy" != true ]]; then
        openlia_record_change "$action" failed "$backup_path" 'health check failed after start'
        openlia_die 'health check failed after start; previous image metadata is retained'
    fi
elif [[ "$action" == start || "$action" == restart ]]; then
    openlia_die 'stack was not started'
fi

final_state=$(openlia_read_state)
openlia_record_change "$action" ok "$backup_path" "state=${final_state}"
if [[ "$json" == true ]]; then
    printf '{"ok":true,"action":%s,"state":%s,"backup":%s}\n' \
        "$(openlia_json_quote "$action")" \
        "$(openlia_json_quote "$(openlia_read_state)")" \
        "$(openlia_json_quote "$backup_path")"
else
    printf 'openlia deploy: action=%s state=%s\n' "$action" "$(openlia_read_state)"
fi
