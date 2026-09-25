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
import { McpClient, type ToolResult } from "./browser";

type ToolState = {
  name: string;
  inputSchema: {
    properties?: Record<string, unknown>;
    required?: string[];
  };
};

type LeaseResult = {
  state: string;
  ticket: string | null;
  lease?: string | null;
  queue_position: number | null;
  expires_at?: number;
  hard_expires_at?: number;
};

function resultJson(client: McpClient, result: ToolResult): LeaseResult {
  return JSON.parse(client.getToolText(result)) as LeaseResult;
}

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
  throw new Error(`openlia-browser did not become healthy at ${url}`);
}

describe("openlia-browser package", () => {
  test("reserves a Node-compatible package boundary", async () => {
    const packageData = await Bun.file(
      new URL("../package.json", import.meta.url),
    ).json();
    expect(packageData.type).toBe("module");
    expect(packageData.name).toBe("@openlia/openlia-browser");
  });

  test("brokers task-token browser leases over Streamable HTTP", async () => {
    const downstreamTransports = new Map<
      string,
      StreamableHTTPServerTransport
    >();
    const downstreamServers = new Set<McpServer>();
    const downstreamArguments: Array<Record<string, unknown>> = [];
    let downstreamActive = 0;
    let maxDownstreamActive = 0;
    let downstreamCalls = 0;
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
                inputSchema: {
                  type: "object",
                  properties: { value: { type: "string" } },
                },
              },
              {
                name: "browser_click",
                description: "Click",
                inputSchema: {
                  type: "object",
                  properties: { value: { type: "string" } },
                },
              },
              {
                name: "browser_evaluate",
                description: "Evaluate",
                inputSchema: {
                  type: "object",
                  properties: { expression: { type: "string" } },
                },
              },
            ],
          }));
          mcp.setRequestHandler(CallToolRequestSchema, async (message) => {
            downstreamCalls += 1;
            downstreamActive += 1;
            maxDownstreamActive = Math.max(
              maxDownstreamActive,
              downstreamActive,
            );
            const args = message.params.arguments ?? {};
            downstreamArguments.push(args);
            try {
              await new Promise((resolve) =>
                setTimeout(resolve, Number(args.delay ?? 45)),
              );
              if (args.fail) throw new Error("recoverable browser error");
              return {
                content: [{ type: "text", text: String(args.value ?? "ok") }],
              };
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
    const dataRoot = mkdtempSync(join(tmpdir(), "openlia-browser-"));
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
        OPENLIA_BROWSER_BIND: "127.0.0.1",
        OPENLIA_BROWSER_PORT: String(proxyPort),
        OPENLIA_BROWSER_MCP_URL: `http://127.0.0.1:${downstreamAddress.port}/mcp`,
        OPENLIA_BROWSER_DATA_ROOT: dataRoot,
        OPENLIA_BROWSER_LEASE_TTL_MS: "120",
        OPENLIA_BROWSER_LEASE_MAX_MS: "300",
        OPENLIA_BROWSER_QUEUE_TTL_MS: "80",
      },
      stdout: "pipe",
      stderr: "pipe",
    });
    const healthUrl = `http://127.0.0.1:${proxyPort}/health`;
    const first = new McpClient(`http://127.0.0.1:${proxyPort}/mcp`);
    const second = new McpClient(`http://127.0.0.1:${proxyPort}/mcp`);
    const third = new McpClient(`http://127.0.0.1:${proxyPort}/mcp`);
    const fourth = new McpClient(`http://127.0.0.1:${proxyPort}/mcp`);
    try {
      await waitForHealth(healthUrl);
      for (const path of [
        "/v1/jobs",
        "/openlia/tools/chatgpt-chat",
        "/openlia/jobs/retired",
      ]) {
        const response = await fetch(`http://127.0.0.1:${proxyPort}${path}`, {
          method: "POST",
          body: "{}",
        });
        expect(response.status).toBe(404);
      }
      const legacySse = await fetch(`http://127.0.0.1:${proxyPort}/sse`);
      expect(legacySse.status).toBe(410);
      const legacyMessages = await fetch(
        `http://127.0.0.1:${proxyPort}/messages?sessionId=legacy`,
        { method: "POST", body: "{}" },
      );
      expect(legacyMessages.status).toBe(410);

      await first.connect();
      const listed = (await first.call("tools/list", {})) as {
        tools: ToolState[];
      };
      const names = listed.tools.map((tool) => tool.name);
      expect(names).toContain("openlia_browser_session_request");
      expect(names).toContain("openlia_browser_session_status");
      expect(names).toContain("openlia_browser_session_touch");
      expect(names).toContain("openlia_browser_session_release");
      expect(names).toContain("openlia_browser_session_cancel");
      expect(names).toContain("browser_click");
      expect(names).not.toContain("browser_evaluate");
      const browserTool = listed.tools.find(
        (tool) => tool.name === "browser_snapshot",
      );
      expect(browserTool?.inputSchema.properties?.lease).toEqual({
        type: "string",
      });
      expect(browserTool?.inputSchema.required).toContain("lease");
      const mutatingTool = listed.tools.find(
        (tool) => tool.name === "browser_click",
      );
      expect(mutatingTool?.inputSchema.properties?.operation_id).toEqual({
        type: "string",
        minLength: 1,
        maxLength: 200,
      });
      expect(mutatingTool?.inputSchema.required).toContain("operation_id");
      const requestTool = listed.tools.find(
        (tool) => tool.name === "openlia_browser_session_request",
      );
      expect(requestTool?.inputSchema.required).not.toContain("lease");
      expect(requestTool?.inputSchema.properties).not.toHaveProperty(
        "ttl_seconds",
      );
      const statusTool = listed.tools.find(
        (tool) => tool.name === "openlia_browser_session_status",
      );
      expect(statusTool?.inputSchema.required).toContain("ticket");

      await expect(first.callTool("browser_snapshot", {})).rejects.toThrow(
        /lease/i,
      );
      expect(downstreamCalls).toBe(0);

      const firstLease = resultJson(
        first,
        await first.callTool("openlia_browser_session_request", {
          purpose: "first test",
          request_key: "task-first",
        }),
      );
      expect(firstLease.state).toBe("active");
      expect(typeof firstLease.lease).toBe("string");
      await expect(
        first.callTool("openlia_browser_session_request", {
          purpose: "unsafe\npurpose",
        }),
      ).rejects.toThrow(/safe/i);
      await expect(
        first.callTool("openlia_browser_session_request", {
          purpose: "valid purpose",
          request_key: "unsafe\nkey",
        }),
      ).rejects.toThrow(/safe/i);
      await expect(
        first.callTool("openlia_browser_session_request", {
          purpose: "model-selected ttl is rejected",
          ttl_seconds: 1,
        }),
      ).rejects.toThrow(/ttl_seconds.*environment defaults/i);
      await expect(
        first.callTool("openlia_browser_session_status", {}),
      ).rejects.toThrow(/ticket/i);
      expect(
        resultJson(
          first,
          await first.callTool("openlia_browser_session_request", {
            purpose: "different purpose is ignored on retry",
            request_key: "task-first",
          }),
        ),
      ).toMatchObject({
        state: "active",
        ticket: firstLease.ticket,
        lease: firstLease.lease,
      });
      expect(
        (await fetch(healthUrl).then((response) => response.json())) as {
          browser_lease: { owner: string; purpose: string };
        },
      ).toMatchObject({
        browser_lease: {
          owner: firstLease.ticket,
          purpose: "first test",
        },
      });

      await expect(
        first.callTool("browser_evaluate", {
          lease: firstLease.lease,
          expression: "document.title",
        }),
      ).rejects.toThrow(/not allowed/i);
      expect(downstreamCalls).toBe(0);

      const clickResult = await first.callTool("browser_click", {
        lease: firstLease.lease,
        operation_id: "click-first",
        value: "click",
      });
      expect(first.getToolText(clickResult)).toBe("click");
      const duplicateClickResult = await second.callTool("browser_click", {
        lease: firstLease.lease,
        operation_id: "click-first",
        value: "click",
      });
      expect(second.getToolText(duplicateClickResult)).toBe("click");
      expect(
        downstreamArguments.filter((args) => args.value === "click"),
      ).toEqual([{ value: "click" }]);
      await expect(
        first.callTool("browser_click", {
          lease: firstLease.lease,
          operation_id: "click-first",
          value: "different",
        }),
      ).rejects.toThrow(/different arguments/i);
      await expect(
        first.callTool("browser_click", {
          lease: firstLease.lease,
          operation_id: "click-failed",
          fail: true,
        }),
      ).rejects.toThrow(/recoverable browser error/i);
      await expect(
        second.callTool("browser_click", {
          lease: firstLease.lease,
          operation_id: "click-failed",
          fail: true,
        }),
      ).rejects.toThrow(/do not retry|reconcile/i);

      const firstCall = first.callTool("browser_snapshot", {
        lease: firstLease.lease,
        value: "first",
        delay: 70,
      });
      await new Promise((resolve) => setTimeout(resolve, 10));
      await expect(
        second.callTool("browser_snapshot", { value: "no lease" }),
      ).rejects.toThrow(/lease/i);
      await second.connect();
      expect(
        resultJson(
          second,
          await second.callTool("openlia_browser_session_status", {
            ticket: firstLease.ticket,
          }),
        ),
      ).toMatchObject({
        state: "active",
        ticket: firstLease.ticket,
        lease: firstLease.lease,
      });
      const touchedFirst = resultJson(
        second,
        await second.callTool("openlia_browser_session_touch", {
          lease: firstLease.lease,
        }),
      );
      expect(touchedFirst.hard_expires_at).toBe(firstLease.hard_expires_at);
      expect(touchedFirst.expires_at).toBeGreaterThan(
        firstLease.expires_at ?? 0,
      );
      const reconnectedCall = second.callTool("browser_snapshot", {
        lease: firstLease.lease,
        value: "reconnected",
        delay: 10,
      });

      const unkeyedRequest = resultJson(
        first,
        await first.callTool("openlia_browser_session_request", {
          purpose: "unkeyed task on same MCP connection",
        }),
      );
      expect(unkeyedRequest.state).toBe("queued");
      expect(unkeyedRequest.ticket).not.toBe(firstLease.ticket);
      expect(
        resultJson(
          first,
          await first.callTool("openlia_browser_session_cancel", {
            ticket: unkeyedRequest.ticket,
          }),
        ).state,
      ).toBe("cancelled");

      expect(first.getToolText(await firstCall)).toBe("first");
      expect(second.getToolText(await reconnectedCall)).toBe("reconnected");
      expect(downstreamArguments.slice(-2)).toEqual([
        { value: "first", delay: 70 },
        { value: "reconnected", delay: 10 },
      ]);

      const secondLeaseRequest = resultJson(
        first,
        await first.callTool("openlia_browser_session_request", {
          purpose: "second test",
          request_key: "task-second",
        }),
      );
      expect(secondLeaseRequest.state).toBe("queued");

      await third.connect();
      const thirdLeaseRequest = resultJson(
        third,
        await third.callTool("openlia_browser_session_request", {
          purpose: "third test",
          request_key: "task-third",
        }),
      );
      expect(secondLeaseRequest.state).toBe("queued");
      expect(secondLeaseRequest.queue_position).toBe(1);
      expect(thirdLeaseRequest.queue_position).toBe(2);
      expect(
        resultJson(
          third,
          await third.callTool("openlia_browser_session_request", {
            purpose: "idempotent third request",
            request_key: "task-third",
          }),
        ),
      ).toMatchObject({ ticket: thirdLeaseRequest.ticket, queue_position: 2 });

      expect(
        resultJson(
          second,
          await second.callTool("openlia_browser_session_request", {
            purpose: "retry queued second task",
            request_key: "task-second",
          }),
        ),
      ).toMatchObject({
        state: "queued",
        ticket: secondLeaseRequest.ticket,
        queue_position: 1,
      });

      const canceled = resultJson(
        second,
        await second.callTool("openlia_browser_session_cancel", {
          ticket: thirdLeaseRequest.ticket,
        }),
      );
      expect(canceled.state).toBe("cancelled");

      const released = resultJson(
        second,
        await second.callTool("openlia_browser_session_release", {
          lease: firstLease.lease,
        }),
      );
      expect(released.state).toBe("released");
      const secondStatus = resultJson(
        first,
        await first.callTool("openlia_browser_session_status", {
          ticket: secondLeaseRequest.ticket,
        }),
      );
      expect(secondStatus.state).toBe("active");
      expect(typeof secondStatus.lease).toBe("string");
      const reusedOperationId = await second.callTool("browser_click", {
        lease: secondStatus.lease,
        operation_id: "click-first",
        value: "second lease click",
      });
      expect(second.getToolText(reusedOperationId)).toBe("second lease click");
      await expect(
        first.callTool("browser_snapshot", { lease: firstLease.lease }),
      ).rejects.toThrow(/lease/i);
      await expect(
        second.callTool("openlia_browser_session_touch", {
          lease: firstLease.lease,
        }),
      ).rejects.toThrow(/lease/i);

      const secondCall = second.callTool("browser_snapshot", {
        lease: secondStatus.lease,
        value: "second",
        delay: 70,
      });
      const fourthRequestPromise = (async () => {
        await fourth.connect();
        return resultJson(
          fourth,
          await fourth.callTool("openlia_browser_session_request", {
            purpose: "expires in queue",
            request_key: "task-fourth",
          }),
        );
      })();
      const fourthRequest = await fourthRequestPromise;
      expect(fourthRequest.state).toBe("queued");
      await new Promise((resolve) => setTimeout(resolve, 100));
      expect(
        resultJson(
          fourth,
          await fourth.callTool("openlia_browser_session_status", {
            ticket: fourthRequest.ticket,
          }),
        ).state,
      ).toBe("idle");
      expect(second.getToolText(await secondCall)).toBe("second");
      await second.callTool("openlia_browser_session_release", {
        lease: secondStatus.lease,
      });

      const thirdAfterCancel = resultJson(
        third,
        await third.callTool("openlia_browser_session_status", {
          ticket: thirdLeaseRequest.ticket,
        }),
      );
      expect(thirdAfterCancel.state).toBe("idle");

      const activeBeforeClose = resultJson(
        third,
        await third.callTool("openlia_browser_session_request", {
          purpose: "close keeps lease",
          request_key: "task-close",
        }),
      );
      expect(activeBeforeClose.state).toBe("active");
      third.close();
      await new Promise((resolve) => setTimeout(resolve, 40));
      await fourth.connect();
      const afterClose = resultJson(
        fourth,
        await fourth.callTool("openlia_browser_session_status", {
          ticket: activeBeforeClose.ticket,
        }),
      );
      expect(afterClose.state).toBe("active");
      expect(afterClose.lease).toBe(activeBeforeClose.lease);
      expect(
        fourth.getToolText(
          await fourth.callTool("browser_snapshot", {
            lease: afterClose.lease,
            value: "expires during call",
            delay: 180,
          }),
        ),
      ).toBe("expires during call");
      expect(
        resultJson(
          fourth,
          await fourth.callTool("openlia_browser_session_status", {
            ticket: activeBeforeClose.ticket,
          }),
        ).state,
      ).toBe("idle");
      await expect(
        fourth.callTool("browser_snapshot", { lease: afterClose.lease }),
      ).rejects.toThrow(/lease/i);
      expect(downstreamArguments.at(-1)).toEqual({
        value: "expires during call",
        delay: 180,
      });

      expect(maxDownstreamActive).toBe(1);
    } catch (error) {
      child.kill("SIGTERM");
      await child.exited;
      const stderr = await new Response(child.stderr).text();
      throw new Error(`${String(error)}\n${stderr}`);
    } finally {
      first.close();
      second.close();
      third.close();
      fourth.close();
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
  }, 20_000);
});
