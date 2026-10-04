# OpenLia End-to-End (E2E) Development Drills

This document provides a battle-tested, repeatable operational runbook for executing end-to-end (E2E) verification drills against disposable or remote development environments using the `openlia_dev` profile.

---

## Overview & Architecture

OpenLia uses a thin control plane architecture:
- **Operator Machine**: Your local workstation where you run the `./openlia` CLI and manage profile configurations (`~/.config/openlia/<profile>/`).
- **Target Machine**: The machine where the Hermes Agent stack runs inside Docker containers (`/opt/<project>/`), managed over SSH (or locally with `--local`).
- **Secure Communication**:
  - Outbound commands and chat arrive via messaging platforms (e.g. Telegram).
  - Private Web UIs (Workspace UI, Open WebUI) are accessible over encrypted P2P or relayed tunnels via [Locho](https://github.com/trchopan/locho).

```
Operator Workstation                       Target Host (e.g. user@test-host)
┌─────────────────────────┐               ┌─────────────────────────────────┐
│ ./openlia CLI           │ ──(SSH Init)─>│ /opt/openlia_dev/               │
│ ~/.config/openlia/dev/  │               │   ├── hermes (Agent Container)  │
│   ├── config.toml       │               │   ├── workspace-ui (Port 8089)  │
│   └── openlia_dev.env   │               │   ├── open-webui (Port 8090)    │
│                         │               │   └── locho-host (Sidecar)      │
│ locho attach            │ <──(Locho)───>│                                 │
└─────────────────────────┘               └─────────────────────────────────┘
```

---

## Prerequisites

On your operator machine:
- Go 1.24+
- Bun (`bun --version`)
- Python 3 (`python3 --version`)
- SSH access with public-key authentication to the target machine (`ssh root@<target-ip>`)
- [Locho](https://github.com/trchopan/locho) CLI installed (`which locho`)

On the target machine:
- Linux (x86_64 or aarch64)
- Docker Engine with Compose v2 (`docker compose version`)

---

## Profile Setup (`~/.config/openlia/dev`)

Starter templates are provided under `examples/profiles/dev/`.

1. Create the dev profile directory:
   ```bash
   mkdir -p ~/.config/openlia/dev
   chmod 700 ~/.config/openlia/dev
   ```

2. Copy the example configuration:
   ```bash
   cp examples/profiles/dev/config.toml.example ~/.config/openlia/dev/config.toml
   cp examples/profiles/dev/openlia_dev.env.example ~/.config/openlia/dev/openlia_dev.env
   chmod 600 ~/.config/openlia/dev/openlia_dev.env
   ```

3. Edit `~/.config/openlia/dev/config.toml`:
   - Set `target = "user@your-host"` to your target SSH connection.
   - Adjust `model`, `output_language`, and `timezone` as desired.
   - Keep `schema = 2`.

4. Edit `~/.config/openlia/dev/openlia_dev.env`:
   - Provide your `COPILOT_GITHUB_TOKEN` (must start with `gho_`, `ghu_`, or `github_pat_`).
   - Provide your dedicated test bot token in `TELEGRAM_BOT_TOKEN`.
   - Provide your Telegram numeric user ID in `TELEGRAM_ALLOWED_USERS`.

> [!WARNING]
> **Token Validation Rules**:
> OpenLia's runtime secret helper (`docker/secret-source.sh`) enforces strict validation. For example, `COPILOT_GITHUB_TOKEN` **must** begin with `gho_`, `ghu_`, or `github_pat_`. If any assignment fails validation, the helper exits with code 1 and Hermes drops **all** secrets. Comment out or remove any unused keys.

---

## Step-by-Step Drill Sequence

### Drill 1: Build Host CLI & Target Operator
Before deploying any release, compile both the host CLI and the cross-compiled Linux operator artifact:

```bash
make build-cli build-operator-linux-amd64
```
*(Use `make build-operator-linux-arm64` if your target is an ARM64/Raspberry Pi host).*

Verify the compiled version:
```bash
./openlia --version
```

---

### Drill 2: Pre-flight & Remote Deployment
1. Verify the target directory is clean before initialization:
   ```bash
   ssh user@test-host "test ! -e /opt/openlia_dev && echo 'Target directory is clean'"
   ```

2. Deploy and initialize the stack:
   ```bash
   OPENLIA_CONFIG=~/.config/openlia/dev/config.toml ./openlia init --json
   ```
   *This packages the current release, uploads it to the target, stages runtime secrets, and starts Compose containers.*

3. Inspect stack status:
   ```bash
   OPENLIA_CONFIG=~/.config/openlia/dev/config.toml ./openlia status
   ```

4. Run provider and runtime diagnostics:
   ```bash
   OPENLIA_CONFIG=~/.config/openlia/dev/config.toml ./openlia doctor --check-providers
   ```
   **Pass Criteria**: All checks report `ok: true`, `hermes_doctor` reports `healthy`, and `provider_request` reports `completed`.

---

### Drill 3: Local Workspace Git Verification
OpenLia maintains the workspace (`/opt/data/workspace`) as a local Git repository for history tracking without network remotes:

```bash
OPENLIA_CONFIG=~/.config/openlia/dev/config.toml ./openlia workspace git status
```
**Pass Criteria**: Returns `{"ok":true,"workspace":"/opt/data/workspace","branch":"main","status":"## main"}`.

---

### Drill 4: Telegram Bot Interactive Verification
1. Confirm the Telegram poller is connected inside the Hermes container:
   ```bash
   ssh user@test-host "docker logs openlia_dev-hermes-1 2>&1 | tail -n 20"
   ```
   Look for: `[Telegram] Connected to Telegram (polling mode)`.

2. Open Telegram and send the following test prompts to your bot:
   - **Persona & Language Check**:
     > `Xin chào, bạn là ai?`
     *Expected*: Agent responds in Vietnamese (or configured language), identifying as OpenLia.
   - **Safe Personal Finance Workflow**:
     > `I spent 15,000 VND on a Banh Mi today`
     *Expected*: Agent invokes the `personal-finance` skill and asks for confirmation or journal category, rather than blindly writing to disk.
   - **Workspace Notes Check**:
     > `Kiểm tra các ghi chú trong workspace`
     *Expected*: Agent reads and summarizes workspace template files.

---

### Drill 5: Locho Host & Web UI Verification
When `[locho-host] enabled = true` is configured, you can expose Workspace UI and Open WebUI over an encrypted tunnel:

1. Generate client attachment credentials:
   ```bash
   OPENLIA_CONFIG=~/.config/openlia/dev/config.toml ./openlia locho-host share \
     --output ~/.config/openlia/dev/locho-attachments.toml
   ```

2. Start the Locho tunnel on your local machine:
   ```bash
   set -a && source ~/.config/openlia/locho/relay.env && set +a
   locho attach \
     --config ~/.config/openlia/dev/locho-attachments.toml \
     --relay-config ~/.config/openlia/locho/relay.toml
   ```

3. Test endpoints locally:
   - **Workspace UI**: [http://127.0.0.1:8089](http://127.0.0.1:8089)
     - Test authentication with the configured password.
     - Test workspace document zip export: `curl -i http://127.0.0.1:8089/api/workspace/export -o /tmp/export.zip`.
   - **Open WebUI**: [http://127.0.0.1:8090](http://127.0.0.1:8090)

---

### Drill 6: Complete Teardown & Post-Flight Cleanup
When testing is complete, tear down all resources to leave both local and target machines clean:

1. Stop your local `locho attach` process (Ctrl+C).
2. Clean up temporary attachment files:
   ```bash
   rm -f ~/.config/openlia/dev/locho-attachments.toml
   ```
3. Run non-interactive uninstallation:
   ```bash
   OPENLIA_CONFIG=~/.config/openlia/dev/config.toml ./openlia uninstall \
     --target user@test-host \
     --project openlia_dev \
     --root /opt/openlia_dev \
     --non-interactive \
     --json
   ```
4. Verify remote host cleanup:
   ```bash
   ssh user@test-host "test ! -e /opt/openlia_dev && docker ps -a --filter name=openlia_dev"
   ```
   **Pass Criteria**: `/opt/openlia_dev` is removed, all `openlia_dev-*` containers are stopped and removed, and the `openlia_dev-private` network is deleted.

---

## Troubleshooting Field Guide

| Symptom / Error | Root Cause | Solution |
| :--- | :--- | :--- |
| `unsupported config schema 1` | OpenLia bumped configuration schema to `2`. | Ensure `schema = 2` is set under `[openlia]` in `config.toml`. |
| `unknown setting "workspace_git.enabled"` | Remote Git repository sync was removed in commit `3bf8dde`. | Remove the `[workspace_git]` table from `config.toml`. |
| `[secrets:command] helper failed; resolving no value: code=1` | An environment variable in `hermes.env` failed `secret-source.sh` regex validation. | Verify `COPILOT_GITHUB_TOKEN` starts with `gho_`, `ghu_`, or `github_pat_`. Comment out any unused or malformed token variables. |
| `locho-host is disabled` | Running `locho-host share` when `locho-host.enabled = false`. | Set `[locho-host] enabled = true` in `config.toml` and run `./openlia deploy` before sharing. |
| `backup encryption key setup failed: operator recovery identity is not ready` | `backup.recipient` is set in `config.toml` but the matching private key file does not exist on disk. | Clear `recipient = ""` in `config.toml` so `openlia init` automatically generates a matching pair at `backup.identity_file`. |
| `env: '/opt/.../ops/healthcheck.sh': No such file or directory` | Attempting to run `status` or `doctor` before running `init`. | Run `openlia init` to stage the release files on the target first. |
