#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

action=create
reason=manual
archive=''
json=false

usage() {
    printf '%s\n' \
        'Usage: backup.sh create [--reason NAME] [--json]' \
        '       backup.sh restore --archive PATH [--json]' \
        '  Backups exclude Docker secrets, Hermes auth files, and Locho capabilities.'
}

if (($#)) && [[ "$1" != -* ]]; then
    action=$1
    shift
fi
while (($#)); do
    case "$1" in
        --reason)
            (($# >= 2)) || { usage >&2; exit 2; }
            reason=$2
            shift
            ;;
        --archive)
            (($# >= 2)) || { usage >&2; exit 2; }
            archive=$2
            shift
            ;;
        --json) json=true ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
    shift
done

case "$action" in create|restore) ;; *) usage >&2; exit 2 ;; esac
openlia_validate_paths
openlia_require_command python3
openlia_require_command tar
openlia_sha256 /dev/null >/dev/null
openlia_ensure_dir "$OPENLIA_BACKUP_ROOT" 700
openlia_ensure_dir "$OPENLIA_META_ROOT" 700
openlia_ensure_dir "$OPENLIA_LOCHO_ROOT" 700

create_backup() {
    local why=$1
    local stamp work archive_tmp manifest_tmp digest
    local service_was_running=false
    openlia_require_safe_component "$why" backup-reason
    [[ -d "$OPENLIA_DATA_ROOT" ]] || openlia_die 'Hermes data directory is missing'
    # Quiesce the SQLite/session writer while capturing a consistent archive.
    if command -v docker >/dev/null 2>&1 && openlia_service_running hermes; then
        openlia_compose stop hermes >/dev/null 2>&1 || openlia_die 'cannot stop Hermes for a consistent backup'
        service_was_running=true
    fi
    stamp=$(date -u +%Y%m%dT%H%M%SZ)
    work=$(mktemp -d "${OPENLIA_BACKUP_ROOT}/.work-${stamp}-XXXXXX")
    chmod 700 "$work"
    archive_tmp="${work}/archive.tar.gz"
    manifest_tmp="${work}/manifest.json"

    printf '{"schema":1,"reason":%s,"created_at":%s,"secret_values":"excluded","capabilities":"excluded"}\n' \
        "$(openlia_json_quote "$why")" \
        "$(openlia_json_quote "$(date -u +%Y-%m-%dT%H:%M:%SZ)")" >"$manifest_tmp"

    # Keep the archive rooted in the runtime directory. The excludes are
    # defense in depth for credentials accidentally created by Hermes.
    if ! tar -czf "$archive_tmp" \
        -C "$OPENLIA_RUNTIME_ROOT" \
        --exclude='secrets' \
        --exclude='locho/*/attachments.toml' \
        --exclude='*.env' \
        --exclude='auth.json' \
        --exclude='*/auth.json' \
        --exclude='pairing' \
        --exclude='*/pairing/*' \
        --exclude='mcp-tokens' \
        --exclude='*/mcp-tokens/*' \
        --exclude='browser-profile' \
        --exclude='*/browser-profile/*' \
        --exclude='logs' \
        --exclude='*/logs/*' \
        --exclude='*.sock' \
        --exclude='*.secret*' \
        hermes meta locho \
        -C "$work" manifest.json; then
        rm -rf "$work"
        if [[ "$service_was_running" == true ]]; then
            openlia_compose up -d --no-deps hermes >/dev/null 2>&1 || true
        fi
        openlia_die 'backup archive creation failed'
    fi

    if ! tar -tzf "$archive_tmp" >/dev/null 2>&1; then
        rm -rf "$work"
        if [[ "$service_was_running" == true ]]; then
            openlia_compose up -d --no-deps hermes >/dev/null 2>&1 || true
        fi
        openlia_die 'backup archive validation failed'
    fi

    digest=$(openlia_sha256 "$archive_tmp" | awk '{print $1}')
    archive="${OPENLIA_BACKUP_ROOT}/openlia-${stamp}-$$.tar.gz"
    mv -f "$archive_tmp" "$archive"
    chmod 600 "$archive"
    printf '{"schema":1,"archive":%s,"sha256":%s,"reason":%s,"created_at":%s,"secrets":"excluded"}\n' \
        "$(openlia_json_quote "$archive")" \
        "$(openlia_json_quote "$digest")" \
        "$(openlia_json_quote "$why")" \
        "$(openlia_json_quote "$(date -u +%Y-%m-%dT%H:%M:%SZ)")" >"${archive}.json"
    chmod 600 "${archive}.json"
    rm -rf "$work"
    if [[ "$service_was_running" == true ]]; then
        openlia_compose up -d --no-deps hermes >/dev/null 2>&1 || openlia_die 'backup completed but Hermes could not be restarted'
    fi
    OPENLIA_LAST_ARCHIVE=$archive
}

if [[ "$action" == create ]]; then
    create_backup "$reason"
    openlia_record_change backup ok "$OPENLIA_LAST_ARCHIVE" "reason=${reason}"
    if [[ "$json" == true ]]; then
        printf '{"ok":true,"archive":%s,"secrets":"excluded"}\n' \
            "$(openlia_json_quote "$OPENLIA_LAST_ARCHIVE")"
    else
        printf 'openlia backup: created %s\n' "$OPENLIA_LAST_ARCHIVE"
    fi
    exit 0
fi

if [[ -z "$archive" ]]; then
    selected_archive=''
    for candidate in "$OPENLIA_BACKUP_ROOT"/openlia-*.tar.gz; do
        [[ -f "$candidate" ]] || continue
        selected_archive=$candidate
    done
    archive=$selected_archive
fi

openlia_require_abs_path "$archive" backup-archive
case "$archive" in
    "${OPENLIA_BACKUP_ROOT}"/*) ;;
    *) openlia_die 'restore archive must be inside the backup directory' ;;
esac
[[ -f "$archive" ]] || openlia_die 'restore archive does not exist'

listing=$(mktemp "${OPENLIA_BACKUP_ROOT}/.listing.XXXXXX")
chmod 600 "$listing"
if ! tar -tzf "$archive" >"$listing" 2>/dev/null; then
    rm -f "$listing"
    openlia_die 'restore archive is not a readable tar archive'
fi
unsafe_member=false
while IFS= read -r member || [[ -n "$member" ]]; do
    case "$member" in
        /*|../*|*/../*|*/..|*' '*|*$'\n'*|*$'\r'*)
            unsafe_member=true
            break
            ;;
    esac
done <"$listing"
rm -f "$listing"
[[ "$unsafe_member" == false ]] || openlia_die 'restore archive contains an unsafe path'

# Reject links, devices, and entries outside the restore roots before any
# extraction. GNU tar path checks alone do not make symlink archives safe.
if ! python3 - "$archive" <<'PY'
import posixpath
import sys
import tarfile

archive = sys.argv[1]
with tarfile.open(archive, "r:gz") as handle:
    for member in handle.getmembers():
        name = member.name.rstrip("/")
        if not name or name == "manifest.json":
            continue
        if name.startswith("/") or name == ".." or name.startswith("../") or "/../" in name:
            raise SystemExit(1)
        if name not in {"hermes", "meta", "locho"} and not name.startswith(("hermes/", "meta/", "locho/")):
            raise SystemExit(1)
        if not (member.isdir() or member.isreg()):
            raise SystemExit(1)
        if posixpath.normpath(name) != name:
            raise SystemExit(1)
PY
then
    openlia_die 'restore archive contains an unsupported or unsafe member'
fi

    openlia_ensure_dir "$OPENLIA_DATA_ROOT" 700
pre_restore=$(
    "${SCRIPT_DIR}/backup.sh" create --reason restore-preflight --json
)
pre_restore_archive=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["archive"])' <<<"$pre_restore")

if command -v docker >/dev/null 2>&1 && [[ -f "$OPENLIA_COMPOSE_FILE" ]]; then
    openlia_compose stop hermes >/dev/null 2>&1 || true
fi
openlia_write_state stopped

parent=$(dirname "$OPENLIA_DATA_ROOT")
staging=$(mktemp -d "${parent}/.restore-XXXXXX")
chmod 700 "$staging"
if ! tar --no-same-owner --no-same-permissions -xzf "$archive" -C "$staging" >/dev/null 2>&1 || [[ ! -d "${staging}/hermes" ]]; then
    rm -rf "$staging"
    openlia_record_change restore failed "$pre_restore_archive" 'restore extraction failed'
    openlia_die 'restore extraction failed; current state was preserved'
fi

old_data="${OPENLIA_BACKUP_ROOT}/pre-restore-hermes-$(date -u +%Y%m%dT%H%M%SZ)-$$"
if ! mv "$OPENLIA_DATA_ROOT" "$old_data"; then
    rm -rf "$staging"
    openlia_record_change restore failed "$pre_restore_archive" 'current data move failed'
    openlia_die 'current data move failed; restore was not applied'
fi
if ! mv "${staging}/hermes" "$OPENLIA_DATA_ROOT"; then
    mv "$old_data" "$OPENLIA_DATA_ROOT"
    rm -rf "$staging"
    openlia_record_change restore failed "$pre_restore_archive" 'restore replacement failed'
    openlia_die 'restore replacement failed; current state was restored'
fi
rm -rf "$staging"
chmod 700 "$OPENLIA_DATA_ROOT"
openlia_record_change restore ok "$pre_restore_archive" "archive=${archive} old_data=${old_data}"

if [[ "$json" == true ]]; then
    printf '{"ok":true,"action":"restore","archive":%s,"state":"stopped","secrets":"supplied_out_of_band"}\n' \
        "$(openlia_json_quote "$archive")"
else
    printf 'openlia backup: restored %s; stack remains stopped\n' "$archive"
fi
