#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
    printf '%s\n' 'ops tests skipped: Docker engine unavailable'
    exit 0
fi
TEST_ROOT=$(mktemp -d)
trap 'rm -rf "$TEST_ROOT"' EXIT

export OPENLIA_RUNTIME_ROOT="${TEST_ROOT}/runtime"
export OPENLIA_PROJECT_NAME=openlia-test
export OPENLIA_COMPOSE_FILE="${ROOT}/docker/compose.yaml"
export OPENLIA_GENERATED_COMPOSE="${TEST_ROOT}/generated.yaml"
export OPENLIA_DATA_ROOT="${OPENLIA_RUNTIME_ROOT}/hermes"
export OPENLIA_LOCHO_ROOT="${OPENLIA_RUNTIME_ROOT}/locho"
export OPENLIA_SECRET_FILE="${OPENLIA_RUNTIME_ROOT}/secrets/hermes.env"
export OPENLIA_BACKUP_ROOT="${OPENLIA_RUNTIME_ROOT}/backups"
export OPENLIA_META_ROOT="${OPENLIA_RUNTIME_ROOT}/meta"
export OPENLIA_STATE_FILE="${OPENLIA_META_ROOT}/stack-state"
export OPENLIA_LOCAL_MODE=true

mkdir -p "$OPENLIA_RUNTIME_ROOT"
"${ROOT}/ops/bootstrap.sh" --json >/dev/null
[[ -d "${OPENLIA_DATA_ROOT}/workspace/inbox" ]]
sentinel="${OPENLIA_DATA_ROOT}/workspace/inbox/sentinel.md"
printf '%s\n' 'keep me' >"$sentinel"
"${ROOT}/ops/profile.sh" sync --json >/dev/null
[[ "$(<"$sentinel")" == 'keep me' ]]
"${ROOT}/ops/backup.sh" create --reason test --json >/dev/null
printf '%s\n' 'ops tests passed'
