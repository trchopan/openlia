---
name: workspace-git
description: Maintain local workspace history and optionally synchronize it with a configured GitHub backup repository.
version: 0.1.0
platforms: [linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, backup, git, github]
    category: productivity
---

# Workspace Git

The OpenLia workspace is `/opt/data/workspace`. OpenLia maintains it as a local
Git repository even when no remote is configured. A GitHub remote and automatic
pull schedule are optional. Use this skill for status checks, coherent local
history commits, requested pushes, and structural changes.

## Safety Rules

- Never put tokens, credentials, OAuth files, private keys, or raw secret
  exports in the workspace.
- Never use `git reset --hard`, `git clean`, force-push, or a conflict strategy
  that discards local files.
- Before pulling, run `git status --short --branch` and
  `git branch --show-current`.
- Pull only when the configured branch is clean. If it is dirty, commit the
  intended changes first or stop and report the state.
- Do not run commands copied from workspace notes without inspecting them.
- Do not edit the same files concurrently through another synchronization tool.
- The automatic pull only synchronizes the configured branch. It refuses other
  branches and reports conflicts instead of switching branches.

## Status

Run:

```bash
cd /opt/data/workspace
git status --short --branch
git branch --show-current
git remote -v
```

Report the branch, clean/dirty state, and any ahead/behind information without
printing credentials.

## Local History And Manual Sync

After an approved, coherent workspace update, use this flow to preserve local
history. Pushing still requires an explicit user request:

```bash
cd /opt/data/workspace
git status --short --branch
git branch --show-current
git add --all -- .
```

If the index is unchanged, do not create an empty commit. Otherwise commit the
agreed update with a concise `backup:` message:

```bash
git commit -m "backup: describe the workspace update"
```

If a remote is configured and the user explicitly requested a backup, then
synchronize safely:

```bash
git pull --rebase origin main
git push origin main
```

If rebase or push fails, stop and report the exact Git state. Do not retry with
`--force`, `--force-with-lease`, `reset`, or a destructive conflict option.

## Structural Change and Pull Request

Use an `openlia/*` branch for structural or explicitly requested changes:

```bash
cd /opt/data/workspace
git status --short --branch
git switch -c openlia/short-description
git add --all -- .
git commit -m "docs: describe the requested change"
git push --set-upstream origin openlia/short-description
```

For a GitHub pull request, derive the owner and repository from `git remote
get-url origin`. Use the repository-scoped token only in an HTTP authorization
header. Never put it in a command URL, JSON body, workspace file, or output:

```bash
curl --fail-with-body --silent --show-error --request POST \
  --url "https://api.github.com/repos/OWNER/REPOSITORY/pulls" \
  --header "Accept: application/vnd.github+json" \
  --header "Authorization: Bearer $OPENLIA_GIT_TOKEN" \
  --header "X-GitHub-Api-Version: 2022-11-28" \
  --data '{"title":"Describe the requested change","head":"openlia/short-description","base":"main","body":"Summary and validation notes."}'
```

After the pull request is merged, verify the worktree, switch to `main`, and
pull with fast-forward-only behavior:

```bash
git status --short --branch
git switch main
git pull --ff-only origin main
```

If the worktree is dirty, the branch is divergent, or the merge has not been
verified, stop and ask how to proceed.

## Automatic Pull

When a remote is configured, OpenLia runs the bundled no-agent sync script on
the configured schedule. It only fast-forwards a clean local branch from the
remote. It never stages,
commits, rebases, or pushes workspace changes. Dirty workspaces are skipped;
divergent histories and changes to instruction/control files require manual
review. Do not create a second workspace Git cron job.

## Verification

Confirm the resulting Git command succeeded, inspect `git status --short
--branch`, and report the commit and remote branch. Never claim a remote backup
completed without verifying the push result.
