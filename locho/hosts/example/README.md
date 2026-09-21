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
openlia attachments map <host> <service> --role playwright-browser
openlia attachments map <host> <service> --role openai-endpoint
```

Or directly in `~/.config/openlia/config.toml` using one `[[services]]` block per
host:

```toml
[[services]]
name = "<host>"
"<service>" = "playwright-browser"

[[services]]
name = "<another-host>"
"<service>" = "openai-endpoint"
```

Supported roles:
- `playwright-browser`: Remote browser automation via Playwright MCP server. Wires `OPENLIA_BROWSER_MCP_URL` and manages `openlia-tools`.
- `openai-endpoint`: OpenAI-compatible local model/gateway (e.g. Ollama, vLLM, GenAI). Wires `OPENLIA_OPENAI_ENDPOINT_URL`.

All attached services are automatically published into the runtime service registry at `/opt/data/services.json` for Hermes and associated tools.
