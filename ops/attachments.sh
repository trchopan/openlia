#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

action=list
host=''
source_file=''
json=false

usage() {
    printf '%s\n' \
        'Usage: attachments.sh list [--json]' \
        '       attachments.sh generate [--json]' \
        '       attachments.sh rotate HOST --source PATH [--json]' \
        '  Capability files are accepted only from paths, never command arguments.'
}

if (($#)) && [[ "$1" != -* ]]; then
    action=$1
    shift
fi
if [[ "$action" == rotate && $# -gt 0 && "$1" != -* ]]; then
    host=$1
    shift
fi
while (($#)); do
    case "$1" in
        --source)
            (($# >= 2)) || { usage >&2; exit 2; }
            source_file=$2
            shift
            ;;
        --json) json=true ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
    shift
done

case "$action" in list|generate|rotate) ;; *) usage >&2; exit 2 ;; esac
openlia_validate_paths
openlia_require_command python3
if [[ "$action" != list ]]; then
    openlia_require_command tar
    openlia_require_command sha256sum
    openlia_ensure_dir "$OPENLIA_LOCHO_ROOT" 700
    openlia_ensure_dir "$OPENLIA_META_ROOT" 700
fi

host_config_path() {
    local name=$1
    printf '%s/%s/attachments.toml\n' "$OPENLIA_LOCHO_ROOT" "$name"
}

attachment_hosts() {
    local directory name config
    for directory in "$OPENLIA_LOCHO_ROOT"/*; do
        [[ -d "$directory" ]] || continue
        name=${directory##*/}
        case "$name" in
            ''|.*|*[!A-Za-z0-9_-]*) continue ;;
        esac
        config=$(host_config_path "$name")
        [[ -f "$config" ]] || continue
        printf '%s\n' "$name"
    done
}

validate_attachment_file() {
    local file=$1
    local line services=0 has_host=false has_listen=false
    [[ -f "$file" && -r "$file" ]] || openlia_die 'attachment source is not a readable file'
    while IFS= read -r line || [[ -n "$line" ]]; do
        case "$line" in
            host_id\ =*|host_id=*) has_host=true ;;
            listen_host\ =*|listen_host=*) has_listen=true ;;
            '[[services]]') services=$((services + 1)) ;;
        esac
    done <"$file"
    [[ "$has_host" == true && "$has_listen" == true && "$services" -gt 0 ]] || \
        openlia_die 'attachment source is missing required Locho fields'
}

write_generated_compose() {
    local output=$1
    local tmp host config service source_json host_count=0 index
    local external_network=${OPENLIA_EXTERNAL_NETWORK:-}
    [[ -z "$external_network" ]] || openlia_require_safe_component "$external_network" external-network
    local -a configured_hosts=()
    [[ "${OPENLIA_API_HOST:-127.0.0.1}" == 127.0.0.1 ]] || openlia_die 'API host must remain 127.0.0.1'
    while IFS= read -r host || [[ -n "$host" ]]; do
        if [[ -n "$host" ]]; then
            configured_hosts+=("$host")
            host_count=$((host_count + 1))
        fi
    done < <(attachment_hosts)
    tmp=$(mktemp "${output}.tmp.XXXXXX")
    {
        if ((host_count == 0)) && [[ "${OPENLIA_API_ENABLED:-false}" != true && -z "$external_network" ]]; then
            printf 'services: {}\n'
        else
            printf 'services:\n'
            if [[ "${OPENLIA_API_ENABLED:-false}" == true || -n "$external_network" ]]; then
                printf '  hermes:\n'
                if [[ "${OPENLIA_API_ENABLED:-false}" == true ]]; then
                    printf '    ports:\n'
                    printf '      - "%s:8642:8642"\n' "${OPENLIA_API_HOST:-127.0.0.1}"
                    printf '    environment:\n'
                    printf '      API_SERVER_ENABLED: "true"\n'
                    printf '      API_SERVER_HOST: "0.0.0.0"\n'
                fi
                if [[ -n "$external_network" ]]; then
                    printf '    networks:\n      - openlia-private\n      - openlia-external\n'
                fi
            fi
        fi
        for ((index = 0; index < host_count; index++)); do
            host=${configured_hosts[index]}
            config=$(host_config_path "$host")
            service="locho-${host}"
            source_json=$(openlia_json_quote "$config")
            printf '  %s:\n' "$service"
            # shellcheck disable=SC2016
            printf '    image: "${OPENLIA_LOCHO_IMAGE:-openlia-locho:v1.2.0-beta.1}"\n'
            printf '    build:\n'
            printf '      context: ..\n'
            printf '      dockerfile: docker/locho.Dockerfile\n'
            printf '      args:\n'
            # shellcheck disable=SC2016
            printf '        LOCHO_BASE_IMAGE: "${LOCHO_BASE_IMAGE:-debian}"\n'
            # shellcheck disable=SC2016
            printf '        LOCHO_BASE_TAG: "${LOCHO_BASE_TAG:-13.4-slim}"\n'
            # shellcheck disable=SC2016
            printf '        LOCHO_BASE_DIGEST: "${LOCHO_BASE_DIGEST:-sha256:109e2c65005bf160609e4ba6acf7783752f8502ad218e298253428690b9eaa4b}"\n'
            printf '        LOCHO_VERSION: "1.2.0-beta.1"\n'
            printf '        LOCHO_X86_64_SHA256: "9d257c856f0a9c8220285db45c28c6227dfa76017d160f74490cfef7bd784ad4"\n'
            printf '        LOCHO_AARCH64_SHA256: "1c0e67b130734467783e5e48a69d3003d218a4da624ba5841f3c5e6840f19c18"\n'
            printf '    command: ["attach", "--config", "/etc/locho/attachments.toml"]\n'
            printf '    restart: unless-stopped\n'
            printf '    read_only: true\n'
            printf '    tmpfs:\n      - /tmp\n'
            printf '    security_opt:\n      - no-new-privileges:true\n'
            printf '    cap_drop: [ALL]\n'
            printf '    volumes:\n'
            printf '      - type: bind\n        source: %s\n        target: /etc/locho/attachments.toml\n        read_only: true\n' "$source_json"
            printf '    networks:\n      - openlia-private\n'
        done
        if [[ -n "$external_network" ]]; then
            printf 'networks:\n'
            printf '  openlia-external:\n'
            printf '    name: "%s"\n' "$external_network"
            printf '    external: true\n'
        fi
    } >"$tmp"
    chmod 600 "$tmp"
    mv -f -- "$tmp" "$output"
}

if [[ "$action" == list ]]; then
    hosts=()
    host_count=0
    while IFS= read -r item || [[ -n "$item" ]]; do
        if [[ -n "$item" ]]; then
            hosts+=("$item")
            host_count=$((host_count + 1))
        fi
    done < <(attachment_hosts)
    if [[ "$json" == true ]]; then
        entries=''
        for ((index = 0; index < host_count; index++)); do
            item=${hosts[index]}
            config=$(host_config_path "$item")
            count=$(awk '/^\[\[services\]\]$/ { n++ } END { print n + 0 }' "$config")
            entry=$(printf '{"host":%s,"configured":true,"service_count":%s,"capabilities":"redacted"}' \
                "$(openlia_json_quote "$item")" "$count")
            [[ -z "$entries" ]] || entries+=,
            entries+=$entry
        done
        printf '{"ok":true,"hosts":[%s],"capabilities":"redacted"}\n' "$entries"
    else
        if ((host_count == 0)); then
            printf '%s\n' 'openlia attachments: no runtime hosts configured'
        else
            for ((index = 0; index < host_count; index++)); do
                item=${hosts[index]}
                count=$(awk '/^\[\[services\]\]$/ { n++ } END { print n + 0 }' "$(host_config_path "$item")")
                printf '%s: services=%s capabilities=redacted\n' "$item" "$count"
            done
        fi
    fi
    exit 0
fi

if [[ "$action" == generate ]]; then
    if [[ -f "$OPENLIA_GENERATED_COMPOSE" ]]; then
        "${SCRIPT_DIR}/backup.sh" create --reason attachments-generate --json >/dev/null
        openlia_backup_file "$OPENLIA_GENERATED_COMPOSE" compose-generated 600
    fi
    write_generated_compose "$OPENLIA_GENERATED_COMPOSE"
    openlia_record_change attachments-generate ok '' 'Compose sidecars regenerated'
    if [[ "$json" == true ]]; then
        printf '%s\n' '{"ok":true,"action":"attachments-generate","capabilities":"redacted"}'
    else
        printf '%s\n' 'openlia attachments: Compose sidecars regenerated'
    fi
    exit 0
fi

openlia_require_safe_component "$host" host
[[ -n "$source_file" ]] || openlia_die 'rotate requires --source PATH'
openlia_require_abs_path "$source_file" attachment-source
case "$source_file" in
    "$OPENLIA_REPO_ROOT"/local|"$OPENLIA_REPO_ROOT"/local/*)
        openlia_die 'attachment source must not be under local/' ;;
esac
validate_attachment_file "$source_file"

target_directory="${OPENLIA_LOCHO_ROOT}/${host}"
target_file=$(host_config_path "$host")
if [[ -f "$target_file" ]]; then
    openlia_backup_file "$target_file" "locho-${host}" 600
    config_backup=$OPENLIA_LAST_BACKUP
else
    config_backup=''
fi
"${SCRIPT_DIR}/backup.sh" create --reason "locho-${host}-rotate" --json >/dev/null
if [[ -f "$OPENLIA_GENERATED_COMPOSE" ]]; then
    openlia_backup_file "$OPENLIA_GENERATED_COMPOSE" compose-generated 600
    generated_backup=$OPENLIA_LAST_BACKUP
    generated_was_present=true
else
    generated_backup=''
    generated_was_present=false
fi
openlia_ensure_dir "$target_directory" 700
chown 10000:10000 "$target_directory"
chmod 700 "$target_directory"

restore_previous_attachment() {
    if [[ -n "$config_backup" ]]; then
        openlia_atomic_copy "$config_backup" "$target_file" 600
    else
        rm -f -- "$target_file"
    fi
    if [[ -n "$generated_backup" ]]; then
        openlia_atomic_copy "$generated_backup" "$OPENLIA_GENERATED_COMPOSE" 600
    elif [[ "$generated_was_present" == false ]]; then
        rm -f -- "$OPENLIA_GENERATED_COMPOSE"
    fi
}

service="locho-${host}"
previous_state=$(openlia_read_state)
if [[ -n "$config_backup" ]]; then
    openlia_compose stop "$service" >/dev/null 2>&1 || true
fi
if ! openlia_atomic_copy "$source_file" "$target_file" 600; then
    openlia_record_change "locho-${host}-rotate" failed "$config_backup" 'attachment replacement failed'
    openlia_die 'attachment replacement failed'
fi
chown 10000:10000 "$target_file"
chmod 600 "$target_file"
if ! write_generated_compose "$OPENLIA_GENERATED_COMPOSE"; then
    restore_previous_attachment
    openlia_record_change "locho-${host}-rotate" failed "$config_backup" 'sidecar configuration replacement failed'
    openlia_die 'sidecar configuration replacement failed; previous attachment was restored'
fi

if ! openlia_compose config --quiet >/dev/null 2>&1; then
    restore_previous_attachment
    openlia_record_change "locho-${host}-rotate" failed "$config_backup" 'generated Compose validation failed'
    openlia_die 'generated Compose validation failed; previous attachment was restored'
fi

if [[ "$previous_state" == running ]]; then
    if ! openlia_compose up -d --no-deps "$service" >/dev/null 2>&1; then
        if [[ -n "$config_backup" ]]; then
            restore_previous_attachment
            openlia_compose up -d --no-deps "$service" >/dev/null 2>&1 || true
            openlia_record_change "locho-${host}-rotate" failed "$config_backup" 'Locho restart failed; previous attachment restored'
            openlia_die 'Locho restart failed; previous attachment was restored'
        fi
        openlia_record_change "locho-${host}-rotate" failed '' 'new Locho sidecar start failed'
        openlia_die 'new Locho sidecar start failed'
    fi
fi

openlia_record_change "locho-${host}-rotate" ok "$config_backup" 'single host sidecar replaced'
if [[ "$json" == true ]]; then
    printf '{"ok":true,"action":"locho-rotate","host":%s,"capabilities":"redacted","restart":"%s"}\n' \
        "$(openlia_json_quote "$host")" "$([[ "$previous_state" == running ]] && printf sidecar-only || printf deferred-until-deploy)"
else
    printf 'openlia attachments: host=%s rotated; capabilities were not displayed\n' "$host"
fi
