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

## Service Mapping & Roles

Discovered services can be assigned roles using `openlia attachments map`:

```bash
openlia attachments list
openlia attachments map <host> <service> --role browser-tools
openlia attachments map <host> <service> --role openai-gateway
```

Or directly in `~/.config/openlia/config.toml` using one `[[services]]` block per
host:

```toml
[[services]]
name = "<host>"
"<service>" = "browser-tools"

[[services]]
name = "<another-host>"
"<service>" = "openai-gateway"
```

Supported roles:
- `browser-tools`: Host-local browser job queue and Playwright MCP proxy. Wires `OPENLIA_BROWSER_MCP_URL` and `OPENLIA_BROWSER_JOBS_URL`, registers the attached SSE MCP proxy directly with Hermes, and disables Hermes' native browser toolset.
- `openai-gateway`: OpenAI-compatible local model/gateway (e.g. Ollama, vLLM, GenAI). Declare its explicit `/v1` URL in the matching `fallback_providers` entry.

All attached services are automatically published into the runtime service registry at `/opt/data/services.json` for Hermes and associated tools.
