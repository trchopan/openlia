import { describe, expect, test } from "bun:test";
import { randomUUID } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Server as McpServer } from "@modelcontextprotocol/sdk/server/index.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import {
  CallToolRequestSchema,
  isInitializeRequest,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { McpClient } from "./browser";

async function availablePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("no port");
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  return address.port;
}

async function waitForHealth(url: string): Promise<Record<string, unknown>> {
  const deadline = Date.now() + 5_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok)
        return (await response.json()) as Record<string, unknown>;
    } catch {
      // The subprocess may still be starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  throw new Error(`browser-tools did not become healthy at ${url}`);
}

describe("browser-tools package", () => {
  test("reserves a Node-compatible package boundary", async () => {
    const packageData = await Bun.file(
      new URL("../package.json", import.meta.url),
    ).json();
    expect(packageData.type).toBe("module");
    expect(packageData.name).toBe("@openlia/browser-tools");
  });

  test("serves Streamable HTTP and releases the browser lease after execution", async () => {
    const downstreamTransports = new Map<
      string,
      StreamableHTTPServerTransport
    >();
    const downstreamServers = new Set<McpServer>();
    let downstreamActive = 0;
    let maxDownstreamActive = 0;
    const downstream = createServer((request, response) => {
      void (async () => {
        const chunks: Buffer[] = [];
        for await (const chunk of request)
          chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
        const parsed = chunks.length
          ? (JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown)
          : undefined;
        const header = request.headers["mcp-session-id"];
        const sessionId = Array.isArray(header) ? header[0] : header;
        let transport = sessionId
          ? downstreamTransports.get(sessionId)
          : undefined;
        if (
          !transport &&
          request.method === "POST" &&
          isInitializeRequest(parsed)
        ) {
          const mcp = new McpServer(
            { name: "mock-playwright", version: "1.0.0" },
            { capabilities: { tools: {} } },
          );
          downstreamServers.add(mcp);
          transport = new StreamableHTTPServerTransport({
            sessionIdGenerator: randomUUID,
            enableJsonResponse: true,
            onsessioninitialized: (id) => {
              if (transport) downstreamTransports.set(id, transport);
            },
          });
          transport.onclose = () => {
            if (transport?.sessionId)
              downstreamTransports.delete(transport.sessionId);
          };
          mcp.setRequestHandler(ListToolsRequestSchema, async () => ({
            tools: [
              {
                name: "browser_snapshot",
                description: "Snapshot",
                inputSchema: { type: "object", properties: {} },
              },
            ],
          }));
          mcp.setRequestHandler(CallToolRequestSchema, async (message) => {
            downstreamActive += 1;
            maxDownstreamActive = Math.max(
              maxDownstreamActive,
              downstreamActive,
            );
            try {
              await new Promise((resolve) => setTimeout(resolve, 120));
              if (message.params.arguments?.fail)
                throw new Error("recoverable browser error");
              return { content: [{ type: "text", text: "snapshot" }] };
            } finally {
              downstreamActive -= 1;
            }
          });
          await mcp.connect(transport as Transport);
        }
        if (!transport) {
          response.writeHead(sessionId ? 404 : 400).end();
          return;
        }
        await transport.handleRequest(request, response, parsed);
      })().catch((error) => response.destroy(error as Error));
    });
    await new Promise<void>((resolve) =>
      downstream.listen(0, "127.0.0.1", resolve),
    );
    const downstreamAddress = downstream.address();
    if (!downstreamAddress || typeof downstreamAddress === "string")
      throw new Error("no downstream port");

    const proxyPort = await availablePort();
    const dataRoot = mkdtempSync(join(tmpdir(), "openlia-browser-tools-"));
    const build = await Bun.build({
      entrypoints: [new URL("./server.ts", import.meta.url).pathname],
      outdir: join(dataRoot, "bundle"),
      target: "node",
      format: "esm",
    });
    if (!build.success)
      throw new Error(build.logs.map((log) => log.message).join("\n"));
    const child = Bun.spawn(["node", join(dataRoot, "bundle", "server.js")], {
      cwd: new URL("../../..", import.meta.url).pathname,
      env: {
        ...process.env,
        BROWSER_TOOLS_BIND: "127.0.0.1",
        BROWSER_TOOLS_PORT: String(proxyPort),
        BROWSER_TOOLS_MCP_URL: `http://127.0.0.1:${downstreamAddress.port}/mcp`,
        BROWSER_TOOLS_DATA_ROOT: dataRoot,
        BROWSER_TOOLS_INTERACTIVE_LEASE_GRACE_MS: "80",
      },
      stdout: "pipe",
      stderr: "pipe",
    });
    const healthUrl = `http://127.0.0.1:${proxyPort}/health`;
    const client = new McpClient(`http://127.0.0.1:${proxyPort}/mcp`);
    try {
      await waitForHealth(healthUrl);
      const legacySse = await fetch(`http://127.0.0.1:${proxyPort}/sse`);
      expect(legacySse.status).toBe(410);
      expect(await legacySse.json()).toMatchObject({
        error: "legacy SSE transport is disabled; use /mcp",
        mcp_endpoint: "/mcp",
      });
      const legacyMessages = await fetch(
        `http://127.0.0.1:${proxyPort}/messages?sessionId=legacy`,
        { method: "POST", body: "{}" },
      );
      expect(legacyMessages.status).toBe(410);
      const beforeConnect = await waitForHealth(healthUrl);
      expect(beforeConnect.mcp_sessions).toBe(0);
      expect((beforeConnect.browser_lease as { busy: boolean }).busy).toBe(
        false,
      );
      await client.connect();
      await client.call("tools/list", {});
      let health = await waitForHealth(healthUrl);
      expect((health.browser_lease as { busy: boolean }).busy).toBe(false);

      const toolCall = client.callTool("browser_snapshot", {});
      const activeDeadline = Date.now() + 2_000;
      do {
        health = await waitForHealth(healthUrl);
        if ((health.browser_lease as { busy: boolean }).busy) break;
        await new Promise((resolve) => setTimeout(resolve, 10));
      } while (Date.now() < activeDeadline);
      expect((health.browser_lease as { busy: boolean }).busy).toBe(true);
      expect(client.getToolText(await toolCall)).toBe("snapshot");
      expect(
        ((await waitForHealth(healthUrl)).browser_lease as { busy: boolean })
          .busy,
      ).toBe(true);
      maxDownstreamActive = 0;
      await Promise.all([
        client.callTool("browser_snapshot", {}),
        client.callTool("browser_snapshot", {}),
      ]);
      expect(maxDownstreamActive).toBe(1);
      const queuedAfterFailure = await Promise.allSettled([
        client.callTool("browser_snapshot", { fail: true }),
        client.callTool("browser_snapshot", {}),
      ]);
      expect(queuedAfterFailure[0]?.status).toBe("rejected");
      expect(queuedAfterFailure[1]?.status).toBe("fulfilled");
      await new Promise((resolve) => setTimeout(resolve, 120));
      expect(
        ((await waitForHealth(healthUrl)).browser_lease as { busy: boolean })
          .busy,
      ).toBe(false);
    } catch (error) {
      child.kill("SIGTERM");
      await child.exited;
      const stderr = await new Response(child.stderr).text();
      throw new Error(`${String(error)}\n${stderr}`);
    } finally {
      client.close();
      child.kill("SIGTERM");
      await child.exited;
      await Promise.all(
        [...downstreamServers].map((server) =>
          server.close().catch(() => undefined),
        ),
      );
      await new Promise<void>((resolve, reject) =>
        downstream.close((error) => (error ? reject(error) : resolve())),
      );
      rmSync(dataRoot, { recursive: true, force: true });
    }
  }, 15_000);
});
