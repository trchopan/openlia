#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

json=false

usage() {
    printf '%s\n' \
        'Usage: uninstall.sh [--json]' \
        '  Stop and remove the marked OpenLia installation root.' \
        '  Docker images and build cache are preserved.'
}

while (($#)); do
    case "$1" in
        --json) json=true ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
    shift
done

openlia_validate_paths
openlia_require_command python3
openlia_require_command docker

install_root=${OPENLIA_INSTALL_ROOT:-}
OPENLIA_NETWORK_NAME=${OPENLIA_NETWORK_NAME:-${OPENLIA_PROJECT_NAME}-private}
openlia_require_abs_path "$install_root" install-root
case "$install_root" in
    /|/bin|/boot|/dev|/etc|/home|/lib|/lib64|/media|/mnt|/opt|/proc|/root|/run|/sbin|/srv|/sys|/tmp|/usr|/var)
        openlia_die "install-root is too broad: ${install_root}" ;;
esac
if [[ "$install_root" == /home/* && "${install_root#/home/}" != */* ]]; then
    openlia_die 'install-root must be below a user home directory, not the home directory itself'
fi
[[ "$OPENLIA_RUNTIME_ROOT" == "${install_root}/runtime" ]] || openlia_die 'runtime root does not match install root'
[[ -L "$install_root" ]] && openlia_die 'install-root must not be a symlink'

if [[ ! -e "$install_root" ]]; then
    if [[ "$json" == true ]]; then
        printf '%s\n' '{"ok":true,"action":"uninstall","state":"absent","images":"preserved"}'
    else
        printf '%s\n' 'openlia uninstall: installation root is already absent; images preserved'
    fi
    exit 0
fi

[[ -d "$install_root" ]] || openlia_die 'install-root is not a directory'
[[ -f "${OPENLIA_META_ROOT}/runtime.json" ]] || openlia_die 'runtime marker is missing; refusing to remove an unmarked root'
python3 - "$install_root" "$OPENLIA_PROJECT_NAME" "$OPENLIA_NETWORK_NAME" "${OPENLIA_META_ROOT}/runtime.json" <<'PY'
import json
import os
import sys

install_root, project, network, marker_path = sys.argv[1:]
with open(marker_path, encoding="utf-8") as handle:
    marker = json.load(handle)
if marker.get("install_root", install_root) != install_root:
    raise SystemExit("installation marker root mismatch")
if marker.get("project", project) != project:
    raise SystemExit("installation marker project mismatch")
if marker.get("network", network) != network:
    raise SystemExit("installation marker network mismatch")
current = os.path.join(install_root, "current")
if os.path.lexists(current):
    resolved = os.path.realpath(current)
    release_root = os.path.realpath(os.path.join(install_root, "releases")) + os.sep
    if not resolved.startswith(release_root):
        raise SystemExit("current release points outside installation root")
PY

if [[ -f "$OPENLIA_COMPOSE_FILE" ]]; then
    openlia_compose down --remove-orphans >/dev/null
else
    containers=$(docker ps -aq --filter "label=com.docker.compose.project=${OPENLIA_PROJECT_NAME}" || true)
    if [[ -n "$containers" ]]; then
        # shellcheck disable=SC2086
        docker rm -f $containers >/dev/null
    fi
fi

remaining_containers=$(docker ps -aq --filter "label=com.docker.compose.project=${OPENLIA_PROJECT_NAME}" || true)
[[ -z "$remaining_containers" ]] || openlia_die 'Compose containers remain; installation root was preserved'

if docker network inspect "$OPENLIA_NETWORK_NAME" >/dev/null 2>&1; then
    network_project=$(docker network inspect "$OPENLIA_NETWORK_NAME" --format '{{index .Labels "com.docker.compose.project"}}' 2>/dev/null || true)
    if [[ "$network_project" == "$OPENLIA_PROJECT_NAME" ]]; then
        docker network rm "$OPENLIA_NETWORK_NAME" >/dev/null
    elif [[ -n "$network_project" ]]; then
        openlia_die 'the configured network belongs to another Compose project; installation root was preserved'
    fi
fi

if docker network inspect "$OPENLIA_NETWORK_NAME" >/dev/null 2>&1; then
    openlia_die 'project network remains; installation root was preserved'
fi

rm -rf "$install_root"
[[ ! -e "$install_root" ]] || openlia_die 'installation root could not be removed'

if [[ "$json" == true ]]; then
    printf '%s\n' '{"ok":true,"action":"uninstall","state":"removed","containers":"removed","network":"removed","images":"preserved"}'
else
    printf '%s\n' 'openlia uninstall: remote installation removed; Docker images and build cache preserved'
fi
