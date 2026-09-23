# Workspace UI

The Workspace UI is a React client and Bun HTTP server for browsing and editing
workspace documents. The client has an explicit `WorkspaceApi` boundary so
frontend work does not require OpenLia, Hermes, Go, or Docker.

## Requirements

- Bun `1.2.22` or newer

Install dependencies from the repository root once:

```sh
bun install
```

## Frontend Development

The default development mode uses a stateful in-memory API. It requires no
backend process and is safe to use for normal UI development:

```sh
cd packages/workspace-ui
bun run dev
```

Open `http://127.0.0.1:5173`. The mock supports editing, saving, Git status,
and the default document flow. Use a scenario when working on a specific UI
state:

```sh
VITE_WORKSPACE_UI_SCENARIO=auth bun run dev
VITE_WORKSPACE_UI_SCENARIO=conflict bun run dev
```

The available scenarios are `auth` and `conflict`.

## Real Backend Development

To exercise the actual Bun filesystem server against a disposable workspace:

```sh
cd packages/workspace-ui
bun run dev:real
```

This starts Vite on port `5173`, the API server on port `8089`, and creates
`.dev-workspace/` if no `OPENLIA_WORKSPACE_ROOT` is supplied. Override the
ports or workspace when needed:

```sh
OPENLIA_WORKSPACE_UI_PORT=8090 \
OPENLIA_WORKSPACE_UI_DEV_PORT=5174 \
bun run dev:real
```

The Vite proxy keeps the browser same-origin with the API, matching production
authentication and origin behavior.

## Checks and Builds

Run these from this directory:

```sh
bun run test
bun run test:watch
bun run check
bun run build
bun run preview
```

`preview` builds the client and server, then serves the production layout on
port `8089` using `.preview-workspace/`. Set
`OPENLIA_WORKSPACE_ROOT` and `OPENLIA_WORKSPACE_UI_PORT` to use another root or
port.

The OpenLia root scripts remain available for full-repository checks and
release packaging. They delegate Workspace UI build, typecheck, and test
commands to this package.
