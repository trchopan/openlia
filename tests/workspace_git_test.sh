#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
SYNC_SCRIPT="${ROOT}/profile/cron/scripts/openlia-workspace-git-sync.sh"
TEST_ROOT=$(mktemp -d)
trap 'rm -rf "$TEST_ROOT"' EXIT

remote="${TEST_ROOT}/remote.git"
publisher="${TEST_ROOT}/publisher"
workspace="${TEST_ROOT}/workspace"

git init --bare "$remote" >/dev/null
git clone "$remote" "$publisher" >/dev/null 2>&1
git -C "$publisher" switch -c main >/dev/null
git -C "$publisher" config user.name Test
git -C "$publisher" config user.email test@example.test
printf '%s\n' initial >"${publisher}/record.md"
git -C "$publisher" add --all -- .
git -C "$publisher" commit -m initial >/dev/null
git -C "$publisher" push --set-upstream origin main >/dev/null

git clone --branch main "$remote" "$workspace" >/dev/null 2>&1
git -C "$workspace" config user.name Test
git -C "$workspace" config user.email test@example.test
git -C "$workspace" config openlia.workspace-branch main

printf '%s\n' local >"${workspace}/local.md"
before_dirty=$(git -C "$workspace" rev-parse HEAD)
dirty_output=$(OPENLIA_WORKSPACE_GIT_ROOT="$workspace" bash "$SYNC_SCRIPT")
[[ "$dirty_output" == *'skipped_dirty'* ]]
[[ "$(git -C "$workspace" rev-parse HEAD)" == "$before_dirty" ]]
if git --git-dir "$remote" show main:local.md >/dev/null 2>&1; then
    printf '%s\n' 'dirty workspace changes were pushed unexpectedly' >&2
    exit 1
fi
rm -f "${workspace}/local.md"

printf '%s\n' remote >"${publisher}/remote.md"
git -C "$publisher" add --all -- .
git -C "$publisher" commit -m remote >/dev/null
git -C "$publisher" push origin main >/dev/null
pull_output=$(OPENLIA_WORKSPACE_GIT_ROOT="$workspace" bash "$SYNC_SCRIPT")
[[ "$pull_output" == *'fast_forwarded'* ]]
[[ "$(<"${workspace}/remote.md")" == remote ]]
[[ "$(git --git-dir "$remote" rev-parse main)" == "$(git -C "$workspace" rev-parse HEAD)" ]]

printf '%s\n' 'remote instruction' >"${publisher}/AGENTS.md"
git -C "$publisher" add --all -- .
git -C "$publisher" commit -m instructions >/dev/null
git -C "$publisher" push origin main >/dev/null
before_protected=$(git -C "$workspace" rev-parse HEAD)
if OPENLIA_WORKSPACE_GIT_ROOT="$workspace" bash "$SYNC_SCRIPT" >/dev/null 2>&1; then
    printf '%s\n' 'protected instruction changes were pulled unexpectedly' >&2
    exit 1
fi
[[ "$(git -C "$workspace" rev-parse HEAD)" == "$before_protected" ]]
[[ ! -e "${workspace}/AGENTS.md" ]]

printf '%s\n' 'workspace Git pull tests passed'
