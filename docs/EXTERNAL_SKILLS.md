# External Skill Repositories

OpenLia can fetch skills from configured GitHub repositories, audit their
contents and locked dependencies, and install selected skills into a deployment.
External skills are separate from the skills bundled under `profile/skills/`.

## Repository Format

A repository uses `openlia-skills.json` at its root. The schema is strict:
unknown fields are rejected, `schema` must be `1`, and `skills` must contain at
least one item.

```json
{
  "schema": 1,
  "skills": [
    {
      "name": "release-notes",
      "path": "skills/release-notes",
      "version": "1.2.0",
      "test": ["python", "scripts/test_skill.py"]
    },
    {
      "name": "issue-summary",
      "path": "skills/issue-summary",
      "version": "0.4.1"
    }
  ]
}
```

Each item has these fields:

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | Yes | Safe skill name containing only letters, numbers, `_`, or `-`. Names must be unique within the manifest. |
| `path` | Yes | Clean, non-root, repository-relative directory. Absolute paths and `..` traversal are rejected. |
| `version` | Yes | Non-empty version label. OpenLia records it but does not enforce semantic versioning. |
| `test` | No | Command represented as an argv array, without shell parsing. If present, its first string must be non-empty; execution rejects an empty array or any empty argument. |

The source manifest may be configured at another safe repository-relative path
through the operator transport, but the host CLI currently creates sources that
use the default `openlia-skills.json` path.

Every listed directory must contain `SKILL.md` with frontmatter beginning and
ending with `---`. At minimum it must declare a `name` exactly matching the
manifest and a non-empty `description`:

```markdown
---
name: release-notes
description: Summarize release notes from supplied project data.
---

# Release Notes
```

Additional frontmatter is allowed. OpenLia does not currently validate a
frontmatter version, platform list, tags, credential declarations, or a broader
skill schema.

Fetched repositories must contain only directories and regular files. OpenLia
rejects symlinks, special files, `.git` metadata, Git submodules, `.gitmodules`,
and Git LFS pointer files. Fetch also normalizes directories to mode `0755` and
files to mode `0644`, so test commands should invoke scripts through their
interpreter rather than rely on executable file bits.

## Locked Dependencies

External dependencies are optional and declared per skill:

- A `pyproject.toml` requires a regular `uv.lock` in the same skill directory.
- A `package.json` requires a regular `bun.lock` in the same skill directory.
- A lockfile without its corresponding project file has no effect.
- `requirements.txt`, npm lockfile variants, install hooks, and arbitrary setup
  commands are not used by the external skill manager.

During audit, OpenLia creates a content-addressed environment from the skill
tree hash and lockfile hashes. Python uses `uv sync --frozen --no-dev
--no-install-project`, followed by `uv export` and `pip-audit`. JavaScript uses
`bun install --frozen-lockfile --production --ignore-scripts`, followed by
`bun audit`. A skill with both project files gets both environments.

The one-shot environment builder runs as the operator account's UID/GID in
local non-root mode and as UID/GID `10000` for remote or root-run operations.
It has all Linux capabilities dropped, `no-new-privileges`, a read-only root
filesystem, a read-only source snapshot, temporary `/tmp` and `/run`, and only
the environment directory writable. It has no deployment secret mount. It
remains on the private OpenLia Compose network because locked packages and
vulnerability data may need to be downloaded.

Installed environments live below the deployment runtime's `skill-envs/`
directory and are linked by skill name. Hermes mounts that directory read-only.
A skill can run a command with its environment through:

```sh
openlia-skill run release-notes -- python scripts/main.py
```

That runner supplies only a minimal environment (`HOME`, `LANG`, `PATH`, Python
isolation variables, and `OPENLIA_WORKSPACE_ROOT`) and runs from the installed
skill directory. Dependencies are not installed globally into Hermes.

## Configure Sources

Source names are stable local IDs. Repository URLs must be credential-free
GitHub HTTPS URLs of the form `https://github.com/OWNER/REPOSITORY` (an optional
`.git` suffix is accepted). Branch values are validated Git refs. The default is
`main`.

```sh
openlia skill-sources add team \
  https://github.com/OWNER/openlia-skills \
  --branch main
openlia skill-sources list
```

`add` and `remove` modify the operator `config.toml`. On an initialized local or
remote deployment, `add` immediately resolves the branch, fetches the snapshot,
and validates the repository and manifest; a failure rolls back the new source.
If a source is persisted before deployment initialization, fetch it after
initialization. The equivalent source entry is:

```toml
[[skill_sources]]
name = "team"
repository = "https://github.com/OWNER/openlia-skills"
branch = "main"
```

The explicit `check [SOURCE]` and `fetch [SOURCE]` commands are diagnostics, not
required setup steps. `check` resolves the configured branch to a 40-character
Git commit without caching repository contents. `fetch` resolves and checks out
that commit, validates the repository and manifest, removes `.git`, computes a
SHA-256 digest, and publishes an immutable snapshot. Omitting `SOURCE` checks or
fetches every configured source. Reusing a cached snapshot requires its stored
digest to validate.

To remove a source:

```sh
openlia skill-sources remove team
```

For an initialized local or remote deployment, removal is refused while a skill
from that source is installed. Removing a source does not delete cached
snapshots.

### Private GitHub Repositories

Put a repository-scoped token in the same protected dotenv file configured by
`[secrets].source`:

```toml
[secrets]
source = "/absolute/path/outside/the/repository/hermes.env"
```

```dotenv
OPENLIA_SKILLS_GIT_TOKEN=github_pat_...
```

The file must already exist as a regular, non-symlink file with mode `0600`.
OpenLia reads the token from the protected deployed secret file and supplies it
to Git through a temporary `GIT_ASKPASS` helper; it does not place the token in
the repository URL or Git command arguments. Use the least-privileged token that
can read the configured repositories. Public repositories need no token.

After adding or changing the token, copy the protected source into an existing
deployment with:

```sh
openlia auth rotate
```

If `[secrets].source` has not yet been persisted, use `openlia auth setup` and
enter the protected dotenv path when prompted. Then add the source; on an
initialized deployment it is fetched and validated automatically:

```sh
openlia skill-sources add team https://github.com/OWNER/private-skills \
  --branch main
openlia skills install team/release-notes
```

## Discover And Diagnose

An initialized deployment fetches a source during `skill-sources add`. Inspect
the resulting catalog directly:

```sh
openlia skills list
openlia skills show team/release-notes
```

Use explicit checks when diagnosing a source, audit, or test failure:

```sh
openlia skill-sources check team
openlia skill-sources fetch team
openlia skills audit team/release-notes
openlia skills test team/release-notes
```

External identifiers use `SOURCE/NAME`. `skills install` always requires this
form. Other external commands also accept an unqualified `NAME` when it matches
exactly one fetched source; use `SOURCE/NAME` when names overlap. Installed skill
names must not conflict with a bundled skill or the protected migration skill.

Read-only discovery and verification need no approval. Mutation approvals are:

| Command | Approval |
| --- | --- |
| `skills install SOURCE/NAME` | Type exactly `install SOURCE/NAME`. |
| `skills update NAME` | Type exactly `update NAME` (using the identifier supplied). |
| `skills uninstall NAME` | Type exactly `uninstall skill NAME`. |
| `skills reset NAME` | Type exactly `reset skill NAME`. |
| `skills migrate apply PROPOSAL_ID` | Type exactly `apply migration PROPOSAL_ID`. |
| `skill-sources add/remove`, `skills fork`, `skills fork-refresh`, and migration `prepare/show/reject` | No interactive prompt. |

The approving mutations reject the host CLI's `--non-interactive` mode. Source
configuration changes and fork metadata changes therefore need the same care
even though they have no prompt.

`skills list` combines bundled skills with fetched external catalog entries and
reports source, commit, version, installation state, available updates, local
modifications, and ownership where applicable. `skills show` reports catalog
metadata and the manifest test array; it does not print external `SKILL.md`.

Install and update run the audit automatically. The diagnostic `skills audit`
command validates the regular-file tree, `SKILL.md`, required lockfiles,
frozen dependency resolution, and Python/Bun vulnerability audits. It records
the source, lockfile, and environment hashes in runtime metadata. An audit is a
structural, reproducibility, and known-vulnerability check, not a review of
skill instructions or source-code behavior.

`skills test` first repeats the audit, then runs the manifest's argv array in a
one-shot container with no network, a read-only source snapshot and dependency
environment, a read-only root filesystem, dropped capabilities, and a minimal
environment. It fails if no `test` command is declared. The command can write
only to the container's temporary paths; it does not run against the installed
skill or personal workspace.

## Install And Update

Install is interactive. It fetches the source, audits the selected skill, and
shows the resolved version, immutable commit, and audit result before asking
for confirmation:

```sh
openlia skills install team/release-notes
# Type exactly: install team/release-notes
```

OpenLia creates a backup, copies the immutable source tree to the Hermes data
directory, records source/commit/version/content/environment provenance and a
base snapshot, activates the audited dependency environment, and pauses and
recreates Hermes when the stack is running. The mutation is transactional and
bound to the commit shown in the plan, so a moving branch cannot silently
change the approved content. Installation enables the skill by default. Use
`openlia skills install team/release-notes --disabled` to install it in the
disabled area without exposing it to Hermes.

For an unchanged managed installation, update is:

```sh
openlia skills update release-notes
# Type exactly: update release-notes
```

Before confirmation, update fetches the configured source, audits its current
snapshot, and displays the current and proposed versions and commits. The
approved mutation is bound to that proposed commit and replaces the installed
tree only if the current tree still matches recorded provenance. It returns
`unchanged` when the source commit has not changed. Local edits to a managed
skill block update and uninstall; either discard them with `reset` or explicitly
record them as a fork.

Uninstall and reset are destructive and use stronger confirmations:

```sh
openlia skills uninstall release-notes
# Type exactly: uninstall skill release-notes

openlia skills reset release-notes
# Type exactly: reset skill release-notes
```

`uninstall` removes the installed tree and its external provenance after making
a backup. `reset` makes a backup, restores the currently recorded upstream base,
and changes ownership back to `managed`; it does not fetch a newer commit.
These approving host CLI mutations reject `--non-interactive`.

## Customize And Migrate

Install a skill before customizing it. Edit its active or disabled installed
tree, then record the customization:

```sh
openlia skills fork release-notes
```

`fork` stores a patch relative to the installed base and marks ownership as
`forked`. It is idempotent: running it for an existing fork refreshes the
recorded fork hash and patch after further intentional edits. `fork-refresh` is
an advanced alias for that refresh-only case:

```sh
openlia skills fork-refresh release-notes
```

Updates verify the recorded fork hash, so rerun `fork` after every intentional
change. `fork-refresh` is valid only for an already forked external skill.
Neither command changes the skill contents.

When the source commit changes, `skills update` does not overwrite a fork. It
fetches and audits the new source, stages old-base/current-fork/new-base context,
and reports `update_available`. Ask Hermes in chat to migrate the skill; the
protected `openlia-skill-migration` system skill uses that staged context and
creates the actual proposal. Do not run a redundant `migrate prepare` after
`skills update` has staged the context. Review and apply the resulting proposal:

```sh
openlia skills migrate show PROPOSAL_ID
openlia skills migrate apply PROPOSAL_ID
# Type exactly: apply migration PROPOSAL_ID
openlia skills migrate reject PROPOSAL_ID
```

`migrate prepare NAME` is an advanced fallback that stages context when an
update has not already done so; it does not itself create a proposal.
`show` is read-only. `apply` rejects proposals with conflicts or stale hashes,
re-audits the proposed external tree, creates a backup, updates the skill and
environment transactionally, and preserves fork ownership. `reject` marks a
pending proposal rejected. Only `apply` requires interactive confirmation.

## Limitations

- Sources are GitHub HTTPS repositories only; SSH, GitLab, embedded credentials,
  submodules, Git LFS content, and symlinks are unsupported.
- The host CLI supports the default root manifest only when adding a source.
- Fetch follows a configured branch or ref and records the resolved commit; it
  does not verify signed commits, tags, publisher identity, or attestations.
- External skill versions are labels. Update detection compares source commits,
  not semantic versions or individual skill contents.
- Dependency audits can download packages and vulnerability data on the private
  Compose network. They do not make untrusted package installation risk-free.
- JavaScript lifecycle scripts are disabled. Development dependencies and the
  Python project itself are not installed.
- Test commands receive no network and no deployment secrets, workspace, or
  writable source tree. Tests that require those resources are unsupported.
- Audit does not establish that instructions or executable code are safe.
  Review the source and the exact commit before approving installation.
- The environment builder runs as the invoking account's UID/GID for local
  non-root deployments and as UID/GID `10000` for remote or root-run
  operations. It has network access for locked packages and vulnerability data,
  but no deployment secret mount. This isolation does not make package
  installation risk-free.
- Cache and content-addressed environment garbage collection is not currently a
  CLI operation; source removal and skill uninstall leave those artifacts.

## Troubleshooting

| Symptom | Action |
| --- | --- |
| Private source authentication fails | Confirm `OPENLIA_SKILLS_GIT_TOKEN` is in the mode-`0600` dotenv at `[secrets].source`, then run `openlia auth rotate`. Use `openlia auth setup` first if the source path is not persisted. Check repository read scope, URL, and branch. |
| Manifest is rejected | Validate root `openlia-skills.json` against the strict schema, reject unknown fields, and ensure every entry points to a safe directory whose `SKILL.md` name matches. Run `openlia skill-sources fetch NAME` to reproduce validation. |
| Audit fails | Add the required `uv.lock` or `bun.lock`, keep dependency resolution frozen, and address reported vulnerability or package failures. `openlia skills audit NAME/SKILL` reruns the diagnostic without installing. |
| Update reports local modifications | Preserve intentional edits with idempotent `openlia skills fork SKILL`; otherwise use `openlia skills reset SKILL` after reviewing its destructive confirmation. Fork updates require a migration proposal rather than overwrite. |
