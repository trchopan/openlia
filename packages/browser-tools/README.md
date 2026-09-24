# Browser Tools

`browser-tools` is the single host-local owner of the authenticated browser.
It exposes the asynchronous job API and the MCP proxy on one loopback listener.

Build the Node-compatible bundle from the repository root:

```sh
bun run build:browser-tools
```

Run it on the browser host:

```sh
BROWSER_TOOLS_MCP_URL=http://127.0.0.1:8931 \
  BROWSER_TOOLS_PORT=8932 \
  BROWSER_TOOLS_SUPERVISE_PLAYWRIGHT=1 \
  BROWSER_TOOLS_PLAYWRIGHT_TOKEN_FILE="$HOME/services/playwright-server-token.txt" \
  BROWSER_TOOLS_PLAYWRIGHT_PORT=8931 \
  BROWSER_TOOLS_DATA_ROOT="$HOME/services/browser-tools/data" \
  node "$HOME/services/browser-tools/server.js"
```

The host service should be supervised as the logged-in browser user and bound
to loopback. Export port `8932` through Locho as the `browser-tools` service;
OpenLia then uses the same endpoint for both `OPENLIA_BROWSER_MCP_URL` and
`OPENLIA_BROWSER_JOBS_URL`.

Raw Playwright MCP must remain loopback-only. OpenLia clients should never be
attached directly to the underlying Playwright endpoint after this service is
enabled.
