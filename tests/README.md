# Smoke Tests

The smoke runner has two separate purposes:

- `--mode cli` exercises the local CLI, bundled skills, Compose syntax, and
  shell syntax with synthetic data.
- `--mode live` deploys the current checkout to an explicitly named disposable
  Linux target and waits for manual Telegram confirmation.

## Local Smoke

Run the safe local gate from the repository root:

```sh
python3 tests/smoke.py --mode cli
```

The local gate does not contact a provider, Telegram, or Locho.

## Live Smoke

Live mode requires every deployment input. It has no target, project, path, or
Locho-host defaults. The only optional behavior is `--cleanup`, which defaults
to `false`.

The source files contain credentials and Locho capabilities. The runner changes
both source files to mode `0600`, stages protected temporary copies outside the
checkout, and never prints their values.

Run against a disposable Linux target with external local inputs. Replace the
target and remote-root placeholders with values for your own environment:

```sh
python3 tests/smoke.py --mode live \
  --target user@example.invalid \
  --project openlia_smoke \
  --remote-root /srv/openlia-smoke \
  --env-file "$HOME/.config/openlia/dev/openlia_dev.env" \
  --attachments-file "$HOME/.config/openlia/dev/locho-attachments.toml" \
  --locho-host genai
```

Run live mode from an interactive terminal so the two Telegram confirmations
can be answered. A non-interactive invocation records those manual cases as
`N/A` rather than claiming that Telegram was tested.

The `--locho-host` value must match the host and service names in your
attachment configuration. The remote root must not already exist; this keeps
the optional cleanup operation scoped to a deployment created by this run.

The runner performs these live checks:

- Upload and initialize the current OpenLia release.
- Rotate the `genai` Locho attachment and reconcile its sidecar.
- Run the provider-backed Hermes health check.
- Confirm the `personal-finance` skill exists in the remote runtime.
- Ask for manual confirmation that Telegram delivered a reply.
- Ask for manual confirmation that the reply followed the finance workflow.

When prompted, send this exact message to the configured Telegram bot:

```text
I spent 15,000 VND on a Banh Mi today
```

The finance skill should not silently create a journal entry. Because the
message does not identify a journal, a safe response may request the journal
path or approval before making a write.

The report is written to a temporary directory outside the checkout unless
`--report` is supplied. Reports redact credential-shaped output.

## Optional Cleanup

The default live command leaves the specified remote root running for manual
inspection. To uninstall the deployment after the interactive checks, add
`--cleanup`. This invokes the same root-aware uninstall operation described
below:

```sh
python3 tests/smoke.py --mode live \
  --target user@example.invalid \
  --project openlia_smoke \
  --remote-root /srv/openlia-smoke \
  --env-file "$HOME/.config/openlia/dev/openlia_dev.env" \
  --attachments-file "$HOME/.config/openlia/dev/locho-attachments.toml" \
  --locho-host genai \
  --cleanup
```

Cleanup is not attempted when the remote root pre-exists or cannot be proven
to be reachable before deployment.

## Remote Uninstall

Use `openlia uninstall` when removing an existing deployment outside a smoke
run. It removes Compose containers, the project network, and the marked
OpenLia installation root. Docker images and build cache are preserved.

The command requires every deployment identifier and asks for confirmation:

```sh
go run . uninstall \
  --target user@example.invalid \
  --project openlia_smoke \
  --remote-root /srv/openlia-smoke
```

For an approved non-interactive removal:

```sh
go run . uninstall \
  --target user@example.invalid \
  --project openlia_smoke \
  --remote-root /srv/openlia-smoke \
  --non-interactive \
  --json
```

Uninstall refuses broad paths, symlink roots, and roots without the OpenLia
runtime marker.

## Other Verification

The broader local checks remain:

```sh
go test ./...
go vet ./...
python3 -m py_compile tests/smoke.py
./tests/ops_test.sh
docker compose -f docker/compose.yaml config --quiet
```
