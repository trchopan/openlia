#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

action=list
source_file=''
json=false

usage() {
    printf '%s\n' \
        'Usage: auth.sh list [--json]' \
        '       auth.sh rotate --source PATH [--json]' \
        '  PATH is a mode-0600 dotenv file; secret values are never arguments or output.'
}

if (($#)) && [[ "$1" != -* ]]; then
    action=$1
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

case "$action" in list|rotate) ;; *) usage >&2; exit 2 ;; esac
openlia_validate_paths
openlia_require_command python3
if [[ "$action" == rotate ]]; then
    openlia_require_command docker
    openlia_require_command tar
    openlia_require_command sha256sum
fi

validate_secret_file() {
    local file=$1
    local line key value count=0
    [[ -f "$file" && -r "$file" ]] || openlia_die 'credential source is not a readable file'
    while IFS= read -r line || [[ -n "$line" ]]; do
        line=${line%$'\r'}
        [[ -z "$line" || "$line" == \#* ]] && continue
        case "$line" in *=*) ;; *) openlia_die 'credential source is not dotenv-shaped' ;; esac
        key=${line%%=*}
        value=${line#*=}
        case "$key" in
            ''|[!A-Za-z_]*|*[!A-Za-z0-9_]*) openlia_die 'credential source contains an invalid key name' ;;
        esac
        case "$key" in
            BASH_ENV|ENV|LD_PRELOAD|LD_LIBRARY_PATH|PATH|PYTHONPATH|NODE_OPTIONS|SHELL|\
            HERMES_HOME|HERMES_ENV|HERMES_CONFIG|HERMES_TIMEZONE|HERMES_YOLO_MODE|HERMES_INTERACTIVE)
                openlia_die 'credential source contains a protected environment key' ;;
        esac
        [[ -n "$value" ]] || openlia_die 'credential source contains an empty value'
        if [[ "$key" == COPILOT_GITHUB_TOKEN ]]; then
            case "$value" in
                gho_*|github_pat_*|ghu_*) ;;
                *) openlia_die 'COPILOT_GITHUB_TOKEN must use a supported OAuth, fine-grained, or GitHub App token' ;;
            esac
        fi
        count=$((count + 1))
    done <"$file"
    ((count > 0)) || openlia_die 'credential source contains no credentials'
}

provider_inventory() {
    local file=$1
    local key line openai_slots=0 copilot=false base_url=false
    [[ -f "$file" ]] || {
        printf '0 0 false\n'
        return
    }
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ "$line" == \#* || "$line" != *=* ]] && continue
        key=${line%%=*}
        case "$key" in
            OPENAI_API_KEY|OPENAI_API_KEY_[2-9]|OPENAI_API_KEY_[1-9][0-9]*) openai_slots=$((openai_slots + 1)) ;;
            COPILOT_GITHUB_TOKEN) copilot=true ;;
            OPENAI_BASE_URL) base_url=true ;;
        esac
    done <"$file"
    printf '%s %s %s\n' "$openai_slots" "$copilot" "$base_url"
}

if [[ "$action" == list ]]; then
    read -r openai_slots copilot base_url < <(provider_inventory "$OPENLIA_SECRET_FILE")
    if [[ "$json" == true ]]; then
        printf '{"ok":true,"providers":[{"name":"openai-api","configured":%s,"slots":%s,"endpoint_configured":%s},{"name":"copilot","configured":%s,"slots":%s}],"secret_values":"redacted"}\n' \
            "$(openlia_json_bool "$([[ "$openai_slots" -gt 0 ]] && printf true || printf false)")" \
            "$openai_slots" \
            "$(openlia_json_bool "$base_url")" \
            "$(openlia_json_bool "$copilot")" \
            "$([[ "$copilot" == true ]] && printf 1 || printf 0)"
    else
        printf 'openai-api: configured=%s slots=%s endpoint=%s\n' \
            "$([[ "$openai_slots" -gt 0 ]] && printf yes || printf no)" "$openai_slots" "$([[ "$base_url" == true ]] && printf yes || printf no)"
        printf 'copilot: configured=%s\n' "$([[ "$copilot" == true ]] && printf yes || printf no)"
    fi
    exit 0
fi

[[ -n "$source_file" ]] || openlia_die 'rotate requires --source PATH'
openlia_require_abs_path "$source_file" credential-source
case "$source_file" in
    "$OPENLIA_REPO_ROOT"/local|"$OPENLIA_REPO_ROOT"/local/*)
        openlia_die 'credential source must not be under local/' ;;
esac
validate_secret_file "$source_file"

openlia_ensure_dir "$OPENLIA_META_ROOT" 700
openlia_ensure_dir "$OPENLIA_RUNTIME_ROOT" 700
chown 10000:10000 "$(dirname -- "$OPENLIA_SECRET_FILE")"
chmod 700 "$(dirname -- "$OPENLIA_SECRET_FILE")"
"${SCRIPT_DIR}/backup.sh" create --reason auth-rotate --json >/dev/null
previous_state=$(openlia_read_state)
if [[ -f "$OPENLIA_SECRET_FILE" ]]; then
    openlia_backup_file "$OPENLIA_SECRET_FILE" auth-secret 600
    secret_backup=$OPENLIA_LAST_BACKUP
else
    secret_backup=''
fi

if ! openlia_compose stop hermes >/dev/null 2>&1; then
    openlia_record_change auth-rotate failed "$secret_backup" 'Hermes stop failed'
    openlia_die 'Hermes stop failed; credentials were not changed'
fi
if ! openlia_atomic_copy "$source_file" "$OPENLIA_SECRET_FILE" 600; then
    openlia_record_change auth-rotate failed "$secret_backup" 'credential replacement failed'
    openlia_die 'credential replacement failed'
fi
chown 10000:10000 "$OPENLIA_SECRET_FILE"
chmod 600 "$OPENLIA_SECRET_FILE"

if [[ "$previous_state" == running ]]; then
    # Compose file-backed secrets are read at container creation time. Recreate
    # only Hermes so an endpoint/key rotation is visible without touching Locho.
    if ! openlia_compose up -d --no-deps --force-recreate hermes >/dev/null 2>&1; then
        if [[ -n "$secret_backup" ]]; then
            openlia_atomic_copy "$secret_backup" "$OPENLIA_SECRET_FILE" 600
        fi
        openlia_compose up -d --no-deps --force-recreate hermes >/dev/null 2>&1 || true
        openlia_record_change auth-rotate failed "$secret_backup" 'Hermes restart failed; previous secret restored'
        openlia_die 'Hermes restart failed; previous secret was restored'
    fi
elif [[ "$previous_state" == stopped ]]; then
	# Remove the stopped container so the next explicit start creates it from the
	# replacement Compose secret instead of reusing an old secret mount.
	openlia_compose rm -f hermes >/dev/null 2>&1 || true
    openlia_write_state stopped
fi

openlia_record_change auth-rotate ok "$secret_backup" 'provider credentials replaced atomically'
if [[ "$json" == true ]]; then
    printf '%s\n' '{"ok":true,"action":"auth-rotate","secret_values":"redacted","restart":"hermes_only"}'
else
    printf '%s\n' 'openlia auth: credentials rotated; values were not displayed'
fi
