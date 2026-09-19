#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=ops/lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

action=setup
remote=''
branch=main
schedule='every 5m'
author_name='OpenLia Agent'
author_email='openlia@localhost'
json=false

usage() {
    printf '%s\n' \
        'Usage: workspace-git.sh setup|ensure --remote URL --branch NAME --schedule SCHEDULE --author-name NAME --author-email EMAIL [--json]' \
        '       workspace-git.sh status [--json]' \
        '  Git credentials are read from the protected OPENLIA_GIT_TOKEN source.'
}

if (($#)) && [[ "$1" != -* ]]; then
    action=$1
    shift
fi
while (($#)); do
    case "$1" in
        --remote)
            (($# >= 2)) || { usage >&2; exit 2; }
            remote=$2
            shift
            ;;
        --branch)
            (($# >= 2)) || { usage >&2; exit 2; }
            branch=$2
            shift
            ;;
        --schedule)
            (($# >= 2)) || { usage >&2; exit 2; }
            schedule=$2
            shift
            ;;
        --author-name)
            (($# >= 2)) || { usage >&2; exit 2; }
            author_name=$2
            shift
            ;;
        --author-email)
            (($# >= 2)) || { usage >&2; exit 2; }
            author_email=$2
            shift
            ;;
        --json) json=true ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
    shift
done

case "$action" in
    setup|ensure|status) ;;
    *) usage >&2; exit 2 ;;
esac

openlia_validate_paths
openlia_require_command docker
openlia_require_command python3
openlia_require_safe_component "$OPENLIA_PROJECT_NAME" project-name

if [[ "$action" != status ]]; then
    python3 - "$remote" "$branch" "$schedule" "$author_name" "$author_email" <<'PY'
import re
import sys
from urllib.parse import urlparse

remote, branch, schedule, author_name, author_email = sys.argv[1:]
parsed = urlparse(remote)
parts = parsed.path.strip("/").split("/")
if (
    parsed.scheme != "https"
    or parsed.hostname != "github.com"
    or parsed.username is not None
    or parsed.query
    or parsed.fragment
    or len(parts) != 2
    or not re.fullmatch(r"[A-Za-z0-9_.-]+", parts[0])
    or not re.fullmatch(r"[A-Za-z0-9_.-]+(?:\.git)?", parts[1])
):
    raise SystemExit("workspace Git remote must be an HTTPS GitHub repository URL")
if (
    not branch
    or branch.startswith((".", "-"))
    or ".." in branch
    or any(character in branch for character in " ~^:?*[\\\"\r\n")
    or not re.fullmatch(r"[A-Za-z0-9._/-]+", branch)
):
    raise SystemExit("workspace Git branch is invalid")
if not schedule or len(schedule) > 120 or any(character in schedule for character in "\r\n"):
    raise SystemExit("workspace Git schedule is invalid")
if not author_name or len(author_name) > 200 or any(character in author_name for character in "\r\n"):
    raise SystemExit("workspace Git author name is invalid")
if not author_email or len(author_email) > 254 or "@" not in author_email or any(character in author_email for character in "\r\n"):
    raise SystemExit("workspace Git author email is invalid")
PY
fi

workspace=/opt/data/workspace
run_git() {
    openlia_compose exec -T -u 10000 -w "$workspace" hermes git "$@"
}

run_hermes() {
    openlia_compose exec -T -u 10000 hermes hermes "$@"
}

require_running() {
    openlia_service_running hermes || openlia_die 'Hermes must be running for workspace Git operations'
    openlia_compose exec -T -u 10000 hermes test -x /opt/data/scripts/openlia-workspace-git-sync.sh >/dev/null 2>&1 || \
        openlia_die 'workspace Git sync script is missing from the Hermes profile'
}

staged_paths_are_safe() {
    local path
    while IFS= read -r path || [[ -n "$path" ]]; do
        case "$path" in
            .env|.env.*|*.env|*.secret|*.secret.*|*.pem|*.key|*.p12|*.pfx|auth.json|*/auth.json|sessions/*|logs/*|cache/*|browser-profile/*|mcp-tokens/*|pairing/*)
                printf 'openlia: workspace Git refused a credential or runtime path: %s\n' "$path" >&2
                return 1
                ;;
        esac
    done < <(run_git diff --cached --name-only)
}

commit_staged_changes() {
    local message=$1
    if ! run_git diff --cached --quiet; then
        staged_paths_are_safe
        run_git commit -m "$message" >/dev/null
    fi
}

configure_cron() {
    local cron_name=openlia-workspace-git
    local edit_output
    if edit_output=$(run_hermes cron edit "$cron_name" \
        --schedule "$schedule" 2>&1); then
        return 0
    fi
    : "$edit_output"
    run_hermes cron create "$schedule" \
        --no-agent \
        --script openlia-workspace-git-sync.sh \
        --workdir "$workspace" \
        --deliver local \
        --name "$cron_name" >/dev/null
}

workspace_git_status() {
    local branch_name remote_url status
    branch_name=$(run_git branch --show-current)
    remote_url=$(run_git remote get-url origin 2>/dev/null || true)
    status=$(run_git status --short --branch)
    if [[ "$json" == true ]]; then
        printf '{"ok":true,"workspace":%s,"branch":%s,"remote":%s,"status":%s}\n' \
            "$(openlia_json_quote "$workspace")" \
            "$(openlia_json_quote "$branch_name")" \
            "$(openlia_json_quote "$remote_url")" \
            "$(openlia_json_quote "$status")"
    else
        printf 'openlia workspace git: branch=%s remote=%s\n%s\n' "$branch_name" "$remote_url" "$status"
    fi
}

if [[ "$action" == status ]]; then
    openlia_require_command python3
    require_running
    if ! run_git rev-parse --git-dir >/dev/null 2>&1; then
        openlia_die 'workspace is not a Git repository'
    fi
    workspace_git_status
    exit 0
fi

require_running

if [[ "$action" == ensure ]]; then
    if ! run_git rev-parse --git-dir >/dev/null 2>&1; then
        openlia_die 'workspace is not a Git repository; run workspace Git setup first'
    fi
    current_branch=$(run_git branch --show-current)
    [[ "$current_branch" == "$branch" ]] || openlia_die "workspace Git is on branch ${current_branch}, expected ${branch}; refusing to switch it"
    existing_remote=$(run_git remote get-url origin 2>/dev/null || true)
    [[ "$existing_remote" == "$remote" ]] || openlia_die 'workspace Git origin differs from the configured remote'
    configure_cron
    if [[ "$json" == true ]]; then
        printf '{"ok":true,"action":"ensure","remote":%s,"branch":%s,"schedule":%s,"automatic_pull":true}\n' \
            "$(openlia_json_quote "$remote")" \
            "$(openlia_json_quote "$branch")" \
            "$(openlia_json_quote "$schedule")"
    else
        printf 'openlia workspace git: verified %s and enabled the %s automatic pull\n' "$remote" "$schedule"
    fi
    exit 0
fi

if ! run_git rev-parse --git-dir >/dev/null 2>&1; then
    run_git init -b "$branch" >/dev/null
fi

current_branch=$(run_git branch --show-current)
head_commit=$(run_git rev-parse --verify HEAD 2>/dev/null || true)
if [[ -z "$head_commit" ]]; then
    if [[ "$current_branch" != "$branch" ]]; then
        run_git branch -M "$branch"
    fi
elif [[ "$current_branch" != "$branch" ]]; then
    openlia_die "workspace Git is on branch ${current_branch}, expected ${branch}; refusing to switch it"
fi

existing_remote=$(run_git remote get-url origin 2>/dev/null || true)
if [[ -n "$existing_remote" && "$existing_remote" != "$remote" ]]; then
    openlia_die 'workspace Git origin differs from the configured remote; refusing to replace it'
fi
if [[ -z "$existing_remote" ]]; then
    run_git remote add origin "$remote"
fi

run_git config --local user.name "$author_name"
run_git config --local user.email "$author_email"
run_git config --local openlia.workspace-branch "$branch"

remote_branch_exists=false
remote_ref_output=''
if remote_ref_output=$(run_git ls-remote --heads origin "$branch" 2>/dev/null); then
    [[ -n "$remote_ref_output" ]] && remote_branch_exists=true
else
    remote_status=$?
    if ((remote_status != 2)); then
        openlia_die 'could not read the configured GitHub remote; check the PAT and repository permissions'
    fi
fi

if [[ -z "$head_commit" ]]; then
    run_git add --all -- .
    staged_paths_are_safe
    run_git commit -m 'chore: initialize OpenLia workspace' >/dev/null
else
    run_git add --all -- .
    staged_paths_are_safe
    commit_staged_changes 'backup: synchronize workspace'
fi

if [[ "$remote_branch_exists" == true ]]; then
    run_git fetch --quiet origin "$branch"
    if run_git merge-base --is-ancestor HEAD "origin/${branch}"; then
        run_git merge --ff-only "origin/${branch}" >/dev/null
    elif ! run_git merge-base --is-ancestor "origin/${branch}" HEAD; then
        if ! run_git merge --no-commit --no-ff --allow-unrelated-histories "origin/${branch}" >/dev/null 2>&1; then
            run_git merge --abort >/dev/null 2>&1 || true
            openlia_die 'workspace Git histories conflict; automatic pull is disabled until the conflict is resolved'
        fi
        if ! staged_paths_are_safe; then
            run_git merge --abort >/dev/null 2>&1 || true
            openlia_die 'workspace Git merge contained a credential or runtime path; automatic pull is disabled'
        fi
        run_git commit -m 'chore: reconcile workspace with remote backup' >/dev/null
    fi
fi

run_git push --set-upstream origin "$branch" >/dev/null
configure_cron

if [[ "$json" == true ]]; then
    printf '{"ok":true,"action":%s,"remote":%s,"branch":%s,"schedule":%s,"initial_push":true,"automatic_pull":true}\n' \
        "$(openlia_json_quote "$action")" \
        "$(openlia_json_quote "$remote")" \
        "$(openlia_json_quote "$branch")" \
        "$(openlia_json_quote "$schedule")"
else
    printf 'openlia workspace git: initialized %s, performed the initial push, and enabled the %s automatic pull\n' "$remote" "$schedule"
fi
