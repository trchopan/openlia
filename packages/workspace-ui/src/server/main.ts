import { join, resolve } from "node:path";
import { createWorkspaceHandler } from "./app";

function portFromEnvironment(value: string | undefined): number {
  const port = Number(value ?? "8089");
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(
      "OPENLIA_WORKSPACE_UI_PORT must be an integer between 1 and 65535",
    );
  }
  return port;
}

const workspaceRoot = resolve(
  process.env.OPENLIA_WORKSPACE_ROOT ?? "/workspace",
);
const bind = process.env.OPENLIA_WORKSPACE_UI_BIND ?? "127.0.0.1";
const port = portFromEnvironment(process.env.OPENLIA_WORKSPACE_UI_PORT);
const authRequired = process.env.OPENLIA_WORKSPACE_UI_AUTH_REQUIRED === "true";
const passwordHashFile = process.env.OPENLIA_WORKSPACE_UI_PASSWORD_HASH_FILE;
const handler = createWorkspaceHandler({
  authRequired,
  passwordHashFile,
  publicOrigin: process.env.OPENLIA_WORKSPACE_UI_PUBLIC_ORIGIN,
  staticRoot: join(import.meta.dir, "public"),
  workspaceRoot,
});
const server = Bun.serve({
  fetch(request, instance) {
    return handler(request, instance.requestIP(request)?.address ?? "global");
  },
  hostname: bind,
  port,
});

console.log(`workspace-ui listening on ${bind}:${port}`);

export { server };
