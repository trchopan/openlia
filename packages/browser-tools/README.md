# Browser Tools

`browser-tools` is the single host-local owner of the authenticated browser.
It exposes the asynchronous job API and the MCP proxy on one loopback listener.

Build the Node-compatible bundle from the repository root:

```sh
bun run build:browser-tools
```

Run it on the browser host:

```sh
BROWSER_TOOLS_ROOT="<path_to_browser_tools_root>" \
BROWSER_TOOLS_TOKEN_FILE="<path_to_extension_token_file>" \
  BROWSER_TOOLS_MCP_URL=http://127.0.0.1:8931/mcp \
  BROWSER_TOOLS_PORT=8932 \
  BROWSER_TOOLS_SUPERVISE_PLAYWRIGHT=1 \
  BROWSER_TOOLS_PLAYWRIGHT_TOKEN_FILE="$BROWSER_TOOLS_TOKEN_FILE" \
  BROWSER_TOOLS_PLAYWRIGHT_PORT=8931 \
  BROWSER_TOOLS_DATA_ROOT="$BROWSER_TOOLS_ROOT/data" \
  node "$BROWSER_TOOLS_ROOT/server.js"
```

The host service should be supervised as the logged-in browser user and bound
to loopback. Export port `8932` through Locho as the `browser-tools` service;
OpenLia then uses the same endpoint for both `OPENLIA_BROWSER_MCP_URL` and
`OPENLIA_BROWSER_JOBS_URL`.

The MCP proxy supports Streamable HTTP at `/mcp`. Legacy `/sse` and `/messages`
requests return `410 Gone` so clients cannot silently use the deprecated
transport.

Raw Playwright MCP must remain loopback-only. OpenLia clients should never be
attached directly to the underlying Playwright endpoint after this service is
enabled.

The supported OpenLia lifecycle is configuration-driven:

```sh
openlia browser-tools configure \
  --target "user@browser-host" \
  --ssh-port 22 \
  --root "<path_to_browser_tools_root>" \
  --extension-token-file "<path_to_extension_token_file>"

openlia browser-tools install
openlia browser-tools start
openlia browser-tools status
```

Use `--local` instead of `--target` when the browser host is the same machine.
The service is detached from the invoking shell but does not automatically
restart after a reboot when no operating-system service manager is installed.

## Connect With Locho

OpenLia uses Locho to reach browser-tools even when the browser and OpenLia
deployment run on the same physical machine. Hermes runs inside Docker and
cannot use the host's loopback address directly.

On the browser host, expose only browser-tools through Locho:

```toml
[[services]]
name = "browser-tools"
type = "tcp"
endpoint = "127.0.0.1:8932"
```

Raw Playwright MCP remains private on `127.0.0.1:8931` and must not be shared
with OpenLia.

Generate the browser-tools capability using Locho, then place it in the
OpenLia attachment file:

```toml
host_id = "<locho_host_id>"
listen_host = "0.0.0.0"

[[services]]
capability = "browser-tools:tcp:<capability>"
listen_port = 8932
```

Map the attached service to its OpenLia role:

```sh
openlia attachments map <host> browser-tools --role browser-tools
openlia deploy
```

Keep capability values outside Git and ordinary command arguments. See
[Locho Host Attachments](../../locho/hosts/example/README.md) for capability
creation, protected attachment files, rotation, and service-role mapping.
