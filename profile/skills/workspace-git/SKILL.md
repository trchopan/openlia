---
name: workspace-git
description: Track approved workspace changes in local Git history.
version: 0.2.0
platforms: [linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, history, git]
    category: productivity
---

# Workspace Git History

The OpenLia workspace is `/opt/data/workspace`. It is maintained as a local
Git repository for history tracking. OpenLia does not configure Git remotes or
synchronize workspace history over the network.

## Safety Rules

- Never put tokens, credentials, OAuth files, private keys, or raw secret
  exports in the workspace.
- Never use `git reset --hard`, `git clean`, or destructive conflict handling.
- Inspect the status and current branch before changing the repository.
- Do not stage unrelated changes or create empty commits.
- Do not add a Git remote or use Git network operations; use OpenLia backups
  for recovery.

## Inspect History

```bash
cd /opt/data/workspace
git status --short --branch
git log -10 --oneline
```

## Record an Approved Change

After an approved, coherent workspace update, stage only the intended files and
commit a concise history entry:

```bash
cd /opt/data/workspace
git status --short --branch
git add -- path/to/approved-file.md
git diff --cached
git commit -m "backup: describe the workspace update"
```

If the index is unchanged, do not create an empty commit. Report the resulting
commit and working-tree status. Use `openlia backup create` to create a durable
encrypted backup.
