#!/usr/bin/env bash
set -Eeuo pipefail

workspace=${OPENLIA_WORKSPACE_GIT_ROOT:-/opt/data/workspace}
lock_directory=/tmp/openlia-workspace-git.lock

if ! mkdir "$lock_directory" 2>/dev/null; then
    lock_pid=''
    if [[ -f "${lock_directory}/pid" ]]; then
        lock_pid=$(<"${lock_directory}/pid")
    fi
    if [[ -n "$lock_pid" ]] && kill -0 "$lock_pid" 2>/dev/null; then
        printf '%s\n' '{"wakeAgent":false,"status":"skipped","reason":"another workspace Git sync is running"}'
        exit 0
    fi
    rm -f "${lock_directory}/pid"
    rmdir "$lock_directory" 2>/dev/null || {
        printf '%s\n' '{"wakeAgent":false,"status":"skipped","reason":"workspace Git sync lock is busy"}'
        exit 0
    }
    mkdir "$lock_directory"
fi
printf '%s\n' "$$" >"${lock_directory}/pid"
trap 'rm -f "${lock_directory}/pid"; rmdir "$lock_directory" 2>/dev/null || true' EXIT

cd "$workspace"
export GIT_TERMINAL_PROMPT=0
export GIT_EDITOR=true

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
    printf '%s\n' '{"wakeAgent":false,"status":"blocked","reason":"workspace is not a Git repository"}'
    exit 1
}

branch=$(git branch --show-current)
expected_branch=${OPENLIA_WORKSPACE_GIT_BRANCH:-main}
configured_branch=$(git config --local --get openlia.workspace-branch || true)
if [[ -n "$configured_branch" ]]; then
    expected_branch=$configured_branch
fi
if [[ "$branch" != "$expected_branch" ]]; then
    printf '%s\n' '{"wakeAgent":false,"status":"skipped_branch"}'
    exit 0
fi

if [[ -n "$(git status --porcelain)" ]]; then
    printf '%s\n' '{"wakeAgent":false,"status":"skipped_dirty"}'
    exit 0
fi

origin_url=$(git remote get-url origin 2>/dev/null || true)
if [[ -z "$origin_url" ]]; then
    printf '%s\n' 'workspace Git sync stopped: origin is not configured' >&2
    exit 1
fi

before=$(git rev-parse HEAD)
if ! git fetch --quiet origin "$expected_branch" >/dev/null 2>&1; then
    git status --short --branch >&2 || true
    printf '%s\n' 'workspace Git sync stopped: remote fetch failed; local files were retained' >&2
    exit 1
fi

remote_head=$(git rev-parse "origin/${expected_branch}")
if [[ "$before" == "$remote_head" ]]; then
    printf '%s\n' '{"wakeAgent":false,"status":"unchanged"}'
    exit 0
fi

if ! git merge-base --is-ancestor HEAD "origin/${expected_branch}"; then
    git status --short --branch >&2 || true
    printf '%s\n' 'workspace Git sync stopped: local and remote histories diverged; local files were retained' >&2
    exit 1
fi

protected_change=false
while IFS= read -r path || [[ -n "$path" ]]; do
    case "$path" in
        AGENTS.md|*/AGENTS.md|CLAUDE.md|*/CLAUDE.md|.cursorrules|*/.cursorrules|.gitignore|*/.gitignore)
            protected_change=true
            break
            ;;
    esac
done < <(git diff --name-only HEAD "origin/${expected_branch}")
if [[ "$protected_change" == true ]]; then
    printf '%s\n' 'workspace Git sync stopped: remote changes include instruction or control files; review them before pulling' >&2
    exit 1
fi

if ! git merge --ff-only "origin/${expected_branch}" >/dev/null 2>&1; then
    git status --short --branch >&2 || true
    printf '%s\n' 'workspace Git sync stopped: fast-forward pull failed; local files were retained' >&2
    exit 1
fi

git status --short --branch >&2 || true
printf '%s\n' '{"wakeAgent":false,"status":"fast_forwarded"}'
