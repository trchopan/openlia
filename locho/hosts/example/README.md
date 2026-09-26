# Locho Host Attachments

Copy `attachments.toml.example` to the runtime path on the selected deployment:

```text
<install-root>/runtime/locho/<host-name>/attachments.toml
```

The runtime file must contain the host ID, `listen_host = "0.0.0.0"`, and one
or more `[[services]]` entries. Replace capability placeholders out of band;
never commit them or pass them as ordinary command arguments. Use
`openlia attachments rotate <host> --source /path/to/attachments.toml` to
validate and replace one host without restarting unrelated sidecars.

Run `openlia` on the operator machine. For a remote deployment, the
`--source` path is read from the operator machine and the validated attachment
file is uploaded to the selected agent runtime. The remote agent machine does
not need the full `openlia` CLI.

## Service Mapping & Roles

Discovered services can be assigned roles using `openlia attachments map`:

```bash
openlia attachments list
openlia attachments map <host> <service> --role openlia-browser
openlia attachments map <host> <service> --role openai-gateway
```

Or directly in the operator machine's `~/.config/openlia/config.toml` using one
`[[services]]` block per host:

```toml
[[services]]
name = "<host>"
"<service>" = "openlia-browser"

[[services]]
name = "<another-host>"
"<service>" = "openai-gateway"
```

Supported roles:
- `openlia-browser`: Host-local Playwright supervisor and MCP proxy. Wires `OPENLIA_BROWSER_MCP_URL`, registers the attached proxy's Streamable HTTP `/mcp` endpoint directly with Hermes, and disables Hermes' native browser toolset. Legacy `/sse` and `/messages` requests are rejected.
- `openai-gateway`: OpenAI-compatible local model/gateway (e.g. Ollama, vLLM, GenAI). Declare its explicit `/v1` URL in the matching `fallback_providers` entry.

All attached services are automatically published into the runtime service registry at `/opt/data/services.json` for Hermes and associated tools.
