#!/usr/bin/env bash
set -Eeuo pipefail

OPENLIA_LIB_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
OPENLIA_OPS_DIR=$(cd "${OPENLIA_LIB_DIR}/.." && pwd -P)
OPENLIA_REPO_ROOT=$(cd "${OPENLIA_OPS_DIR}/.." && pwd -P)

# These are host paths. Compose receives the same values through exported
# variables, while the containers use fixed paths under /opt/data and /etc.
OPENLIA_RUNTIME_ROOT=${OPENLIA_RUNTIME_ROOT:-/srv/openlia/runtime}
OPENLIA_RUNTIME_ROOT=${OPENLIA_RUNTIME_ROOT%/}
[[ -n "$OPENLIA_RUNTIME_ROOT" ]] || OPENLIA_RUNTIME_ROOT=/
OPENLIA_PROJECT_NAME=${OPENLIA_PROJECT_NAME:-openlia}
OPENLIA_INSTALL_ROOT=${OPENLIA_INSTALL_ROOT:-${OPENLIA_RUNTIME_ROOT%/runtime}}
OPENLIA_NETWORK_NAME=${OPENLIA_NETWORK_NAME:-${OPENLIA_PROJECT_NAME}-private}
OPENLIA_COMPOSE_FILE=${OPENLIA_COMPOSE_FILE:-${OPENLIA_REPO_ROOT}/docker/compose.yaml}
OPENLIA_COMPOSE_PROJECT_DIR=${OPENLIA_COMPOSE_PROJECT_DIR:-${OPENLIA_REPO_ROOT}/docker}
OPENLIA_GENERATED_COMPOSE=${OPENLIA_GENERATED_COMPOSE:-${OPENLIA_REPO_ROOT}/docker/compose.generated.yaml}
OPENLIA_DATA_ROOT=${OPENLIA_DATA_ROOT:-${OPENLIA_RUNTIME_ROOT}/hermes}
OPENLIA_LOCHO_ROOT=${OPENLIA_LOCHO_ROOT:-${OPENLIA_RUNTIME_ROOT}/locho}
OPENLIA_SECRET_DIR=${OPENLIA_SECRET_DIR:-${OPENLIA_RUNTIME_ROOT}/secrets}
OPENLIA_SECRET_FILE=${OPENLIA_SECRET_FILE:-${OPENLIA_RUNTIME_ROOT}/secrets/hermes.env}
OPENLIA_BACKUP_ROOT=${OPENLIA_BACKUP_ROOT:-${OPENLIA_RUNTIME_ROOT}/backups}
OPENLIA_META_ROOT=${OPENLIA_META_ROOT:-${OPENLIA_RUNTIME_ROOT}/meta}
OPENLIA_STATE_FILE=${OPENLIA_STATE_FILE:-${OPENLIA_META_ROOT}/stack-state}
OPENLIA_LOCAL_MODE=${OPENLIA_LOCAL_MODE:-false}

export OPENLIA_RUNTIME_ROOT OPENLIA_PROJECT_NAME OPENLIA_DATA_ROOT OPENLIA_SECRET_DIR OPENLIA_SECRET_FILE

# shellcheck source=ops/lib/json.sh
source "${OPENLIA_LIB_DIR}/json.sh"

openlia_die() {
    printf 'openlia: %s\n' "$*" >&2
    exit 1
}

openlia_require_command() {
    command -v "$1" >/dev/null 2>&1 || openlia_die "required command is unavailable: $1"
}

openlia_sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$@"
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$@"
    else
        openlia_die 'required command is unavailable: sha256sum or shasum'
    fi
}

openlia_directory_sha256() {
    local directory=$1
    [[ -d "$directory" && ! -L "$directory" ]] || openlia_die "directory does not exist: $directory"
    python3 - "$directory" <<'PY'
import hashlib
import os
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
digest = hashlib.sha256()
paths = sorted(path for path in root.rglob("*") if "__pycache__" not in path.parts and path.suffix != ".pyc")
for path in paths:
    digest.update(path.relative_to(root).as_posix().encode("utf-8"))
    digest.update(b"\0")
    if path.is_symlink():
        digest.update(b"symlink\0")
        digest.update(os.readlink(path).encode("utf-8"))
    elif path.is_file():
        digest.update(b"file\0")
        with path.open("rb") as handle:
            for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                digest.update(chunk)
    else:
        digest.update(b"directory\0")
    digest.update(b"\0")
print(digest.hexdigest())
PY
}

openlia_file_mode() {
    local path=$1
    if stat -c '%a' "$path" >/dev/null 2>&1; then
        stat -c '%a' "$path"
    else
        stat -f '%Lp' "$path"
    fi
}

openlia_directory_empty() {
    python3 - "$1" <<'PY'
import os
import sys

directory = sys.argv[1]
raise SystemExit(0 if not os.listdir(directory) else 1)
PY
}

openlia_set_runtime_owner() {
    local path=$1
    # Local deployments use the container's Linux user/entrypoint to normalize
    # bind-mounted ownership. Never require the host operator to chown to the
    # container UID, which is unavailable to ordinary Linux users and is not
    # meaningful on Docker Desktop filesystems.
    if [[ "$OPENLIA_LOCAL_MODE" == true ]]; then
        return 0
    fi
    chown 10000:10000 "$path"
}

openlia_require_safe_component() {
    local value=${1:-}
    local label=${2:-component}
    case "$value" in
        ''|.|..|.*|*[!A-Za-z0-9_-]*) openlia_die "invalid ${label}" ;;
    esac
}

openlia_require_abs_path() {
    local path=${1:-}
    local label=${2:-path}
    case "$path" in
        /*) ;;
        *) openlia_die "${label} must be absolute" ;;
    esac
    case "$path" in
    *$'\n'*|*$'\r'*|*$'\t'*) openlia_die "${label} contains unsupported whitespace" ;;
        */../*|*/..|*/./*|*/.) openlia_die "${label} contains an unsafe path component" ;;
    esac
}

openlia_validate_paths() {
    local root=${OPENLIA_RUNTIME_ROOT}
    openlia_require_abs_path "$root" runtime-root
    openlia_require_safe_component "$OPENLIA_PROJECT_NAME" project-name
    openlia_require_abs_path "$OPENLIA_COMPOSE_FILE" compose-file
    openlia_require_abs_path "$OPENLIA_GENERATED_COMPOSE" generated-compose
    case "$OPENLIA_COMPOSE_FILE" in
        "${OPENLIA_REPO_ROOT}/local"|"${OPENLIA_REPO_ROOT}/local"/*)
            openlia_die 'compose-file must not be under local/' ;;
    esac
    case "$OPENLIA_GENERATED_COMPOSE" in
        "${OPENLIA_REPO_ROOT}/local"|"${OPENLIA_REPO_ROOT}/local"/*)
            openlia_die 'generated-compose must not be under local/' ;;
    esac

    case "$root" in
        /|/bin|/boot|/dev|/etc|/home|/lib|/lib64|/media|/mnt|/opt|/proc|/root|/run|/sbin|/srv|/sys|/tmp|/usr|/var)
            openlia_die "runtime-root is too broad: ${root}" ;;
        "${OPENLIA_REPO_ROOT}"|"${OPENLIA_REPO_ROOT}"/*)
            openlia_die "runtime-root must not be inside the repository" ;;
    esac

    local path
    for path in "$OPENLIA_DATA_ROOT" "$OPENLIA_LOCHO_ROOT" "$OPENLIA_SECRET_FILE" "$OPENLIA_BACKUP_ROOT" "$OPENLIA_META_ROOT" "$OPENLIA_STATE_FILE"; do
        openlia_require_abs_path "$path" runtime-path
        case "$path" in
            "${root}"/*) ;;
            *) openlia_die 'runtime paths must remain below runtime-root' ;;
        esac
        case "$path" in
            "${OPENLIA_REPO_ROOT}/local"|"${OPENLIA_REPO_ROOT}/local"/*)
                openlia_die "runtime path must not touch local/" ;;
        esac
    done
}

openlia_ensure_dir() {
    local path=$1
    local mode=${2:-700}
    mkdir -p "$path"
    chmod "$mode" "$path"
}

openlia_atomic_stdin() {
    local destination=$1
    local mode=$2
    local parent tmp
    parent=$(dirname "$destination")
    [[ -d "$parent" ]] || openlia_die "atomic replacement parent does not exist"
    tmp=$(mktemp "${destination}.tmp.XXXXXX")
    if ! cat >"$tmp"; then
        rm -f "$tmp"
        return 1
    fi
    chmod "$mode" "$tmp"
    mv -f "$tmp" "$destination"
}

openlia_atomic_copy() {
    local source=$1
    local destination=$2
    local mode=$3
    local parent tmp
    [[ -f "$source" ]] || openlia_die "source file does not exist"
    parent=$(dirname "$destination")
    [[ -d "$parent" ]] || openlia_die "atomic replacement parent does not exist"
    tmp=$(mktemp "${destination}.tmp.XXXXXX")
    if ! cp "$source" "$tmp"; then
        rm -f "$tmp"
        return 1
    fi
    chmod "$mode" "$tmp"
    mv -f "$tmp" "$destination"
}

openlia_atomic_copy_dir() {
    local source=$1
    local destination=$2
    local parent tmp backup
    [[ -d "$source" && ! -L "$source" ]] || openlia_die "source directory does not exist"
    [[ -d "$destination" && ! -L "$destination" ]] || openlia_die "destination directory does not exist"
    parent=$(dirname "$destination")
    [[ -d "$parent" ]] || openlia_die "atomic replacement parent does not exist"
    tmp=$(mktemp -d "${destination}.tmp.XXXXXX")
    if ! cp -a "${source}/." "$tmp/"; then
        rm -rf "$tmp"
        return 1
    fi
    backup=$(mktemp -d "${destination}.backup.XXXXXX")
    rmdir "$backup"
    if ! mv "$destination" "$backup"; then
        rm -rf "$tmp"
        return 1
    fi
    if ! mv "$tmp" "$destination"; then
        mv "$backup" "$destination" || true
        rm -rf "$tmp"
        return 1
    fi
    rm -rf "$backup"
}

openlia_backup_file() {
    local source=$1
    local label=$2
    local mode=${3:-600}
    local stamp destination
    openlia_require_safe_component "$label" backup-label
    [[ -f "$source" ]] || openlia_die "cannot back up a missing file"
    openlia_ensure_dir "$OPENLIA_BACKUP_ROOT" 700
    stamp=$(date -u +%Y%m%dT%H%M%SZ)
    destination="${OPENLIA_BACKUP_ROOT}/${label}-${stamp}-$$"
    cp "$source" "$destination"
    chmod "$mode" "$destination"
    # shellcheck disable=SC2034
    OPENLIA_LAST_BACKUP=$destination
}

openlia_read_state() {
    if [[ ! -f "$OPENLIA_STATE_FILE" ]]; then
        printf 'never-started\n'
        return
    fi
    local state
    state=$(<"$OPENLIA_STATE_FILE")
    case "$state" in
        never-started|running|stopped) printf '%s\n' "$state" ;;
        *) openlia_die "invalid stack state" ;;
    esac
}

openlia_write_state() {
    case "$1" in
        never-started|running|stopped) ;;
        *) openlia_die "invalid stack state" ;;
    esac
    openlia_atomic_stdin "$OPENLIA_STATE_FILE" 600 <<<"$1"
}

openlia_record_change() {
    local action=$1
    local status=$2
    local backup=${3:-}
    local detail=${4:-}
    local action_json status_json backup_json detail_json timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    action_json=$(openlia_json_quote "$action")
    status_json=$(openlia_json_quote "$status")
    backup_json=$(openlia_json_quote "$backup")
    detail_json=$(openlia_json_quote "$detail")
    openlia_atomic_stdin "${OPENLIA_META_ROOT}/last-change.json" 600 <<EOF
{"schema":1,"action":${action_json},"status":${status_json},"backup":${backup_json},"detail":${detail_json},"recorded_at":$(openlia_json_quote "$timestamp")}
EOF
}

openlia_compose() {
    local -a args
    args=(--project-name "$OPENLIA_PROJECT_NAME" --project-directory "$OPENLIA_COMPOSE_PROJECT_DIR" -f "$OPENLIA_COMPOSE_FILE")
    if [[ -f "$OPENLIA_GENERATED_COMPOSE" ]]; then
        args+=(-f "$OPENLIA_GENERATED_COMPOSE")
    fi
    docker compose "${args[@]}" "$@"
}

openlia_compose_quiet() {
    openlia_compose "$@" >/dev/null 2>&1
}

openlia_service_running() {
    local service=$1
    local running
    running=$(openlia_compose ps --services --filter status=running 2>/dev/null || true)
    case $'\n'"$running"$'\n' in
        *$'\n'"$service"$'\n'*) return 0 ;;
        *) return 1 ;;
    esac
}
