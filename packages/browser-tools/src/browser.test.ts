import { describe, expect, test } from "bun:test";
import { randomUUID } from "node:crypto";
import { createServer } from "node:http";
import { Server as McpServer } from "@modelcontextprotocol/sdk/server/index.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import {
  CallToolRequestSchema,
  isInitializeRequest,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import {
  buildConversationYaml,
  buildRouteYaml,
  cleanUrl,
  currentTabIndex,
  mapsRouteUrl,
  McpClient,
  openOwnedTab,
} from "./browser";

describe("browser compatibility helpers", () => {
  test("normalizes the MCP host header independently of the relay hostname", () => {
    expect(new McpClient("http://locho-browser:9000").host).toBe(
      "localhost:9000",
    );
    expect(new McpClient("http://remote-browser").host).toBe("localhost:8931");
    expect(new McpClient("http://remote-browser").baseUrl).toBe(
      "http://remote-browser/mcp",
    );
  });

  test("uses Streamable HTTP for MCP initialization and tool calls", async () => {
    const transports = new Map<string, StreamableHTTPServerTransport>();
    let toolCalls = 0;
    let connections = 0;
    let terminated = false;
    const httpServer = createServer((request, response) => {
      void (async () => {
        const chunks: Buffer[] = [];
        for await (const chunk of request)
          chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
        const parsed = chunks.length
          ? (JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown)
          : undefined;
        const rpc = parsed as
          | {
              method?: string;
              params?: { arguments?: Record<string, unknown> };
            }
          | undefined;
        if (
          rpc?.method === "tools/call" &&
          rpc.params?.arguments?.transportFail
        ) {
          response.writeHead(404).end("expired session");
          return;
        }
        const header = request.headers["mcp-session-id"];
        const sessionId = Array.isArray(header) ? header[0] : header;
        let transport = sessionId ? transports.get(sessionId) : undefined;
        if (
          !transport &&
          request.method === "POST" &&
          isInitializeRequest(parsed)
        ) {
          connections += 1;
          const server = new McpServer(
            { name: "mock-playwright", version: "1.0.0" },
            { capabilities: { tools: {} } },
          );
          transport = new StreamableHTTPServerTransport({
            sessionIdGenerator: randomUUID,
            enableJsonResponse: true,
            onsessioninitialized: (id) => {
              if (transport) transports.set(id, transport);
            },
          });
          transport.onclose = () => {
            terminated = true;
            if (transport?.sessionId) transports.delete(transport.sessionId);
          };
          server.setRequestHandler(ListToolsRequestSchema, async () => ({
            tools: [
              {
                name: "browser_snapshot",
                description: "Snapshot",
                inputSchema: { type: "object", properties: {} },
              },
            ],
          }));
          server.setRequestHandler(CallToolRequestSchema, async (message) => {
            toolCalls += 1;
            if (message.params.arguments?.fail)
              throw new Error("simulated downstream failure");
            if (message.params.name === "browser_tabs") {
              if (message.params.arguments?.action === "new")
                throw new Error("tab creation unavailable");
              if (message.params.arguments?.action === "list")
                return {
                  content: [
                    {
                      type: "text",
                      text: "- 0: https://example.test/ (current)",
                    },
                  ],
                };
            }
            if (
              message.params.name === "browser_navigate" &&
              String(message.params.arguments?.url).includes("fallback-fails")
            )
              throw new Error("fallback navigation failed");
            return {
              content: [
                { type: "text", text: String(message.params.arguments?.value) },
              ],
            };
          });
          await server.connect(transport as Transport);
        }
        if (!transport) {
          response.writeHead(sessionId ? 404 : 400).end();
          return;
        }
        await transport.handleRequest(request, response, parsed);
      })().catch((error) => response.destroy(error as Error));
    });
    await new Promise<void>((resolve) =>
      httpServer.listen(0, "127.0.0.1", resolve),
    );
    try {
      const address = httpServer.address();
      if (!address || typeof address === "string") throw new Error("no port");
      const client = new McpClient(`http://127.0.0.1:${address.port}/mcp`);
      await Promise.all([client.connect(), client.connect()]);
      const listed = (await client.call("tools/list", {})) as {
        tools: Array<{ name: string }>;
      };
      expect(listed.tools.map((tool) => tool.name)).toEqual([
        "browser_snapshot",
      ]);
      const result = await client.callTool("browser_snapshot", { value: "ok" });
      expect(client.getToolText(result)).toBe("ok");
      expect(toolCalls).toBe(1);
      await expect(
        client.callTool("browser_snapshot", { fail: true }),
      ).rejects.toThrow("simulated downstream failure");
      expect(
        client.getToolText(
          await client.callTool("browser_snapshot", {
            value: "same-session",
          }),
        ),
      ).toBe("same-session");
      expect(connections).toBe(1);
      expect(
        await openOwnedTab(client, "https://gemini.google.com/app"),
      ).toEqual({ index: 0, created: false });
      await expect(
        openOwnedTab(client, "https://fallback-fails.example/"),
      ).rejects.toThrow("tab creation unavailable");
      expect(connections).toBe(1);
      await expect(
        client.callTool("browser_snapshot", { transportFail: true }),
      ).rejects.toThrow("expired session");
      expect(
        client.getToolText(
          await client.callTool("browser_snapshot", { value: "recovered" }),
        ),
      ).toBe("recovered");
      expect(connections).toBe(2);
      client.close();
      await new Promise((resolve) => setTimeout(resolve, 20));
      expect(terminated).toBe(true);
    } finally {
      await new Promise<void>((resolve, reject) =>
        httpServer.close((error) => (error ? reject(error) : resolve())),
      );
    }
  });

  test("builds a schema-1 conversation with deduplicated references", () => {
    const output = buildConversationYaml(
      {
        platform: "gemini",
        topic: "test",
        model: "Gemini",
        url: "https://gemini.google.com/app",
      },
      [
        { role: "user", turn: 1, content: "Question", references: [] },
        {
          role: "assistant",
          turn: 1,
          content: "Answer\nwith detail",
          references: [
            { title: "Docs", url: "https://example.test/page?utm_source=test" },
            { title: "Docs", url: "https://example.test/page" },
          ],
        },
      ],
      "2026-09-22T00:00:00.000000+00:00",
    );
    expect(output).toContain("schema: 1");
    expect(output).toContain("platform: gemini");
    expect(output).toContain("mode: temporary");
    expect(output).toContain("https://example.test/page");
    expect(output).not.toContain("utm_source");
    expect(output).toContain("content: |");
  });

  test("unwraps Google citation redirects before sanitizing tracking fields", () => {
    expect(
      cleanUrl(
        "https://www.google.com/url?q=https%3A%2F%2Fexample.test%2Fdocs%3Futm_source%3Dgoogle&sa=U",
      ),
    ).toBe("https://example.test/docs");
  });

  test("tracks the explicit current tab without relying on tab order", () => {
    expect(
      currentTabIndex(
        "- 0: https://maps.google.com/\n- 4: https://chatgpt.com/ (current)\n",
      ),
    ).toBe(4);
  });

  test("builds a queued Maps route URL and YAML result", () => {
    const url = mapsRouteUrl("Home", "Office", "driving");
    expect(url).toContain("origin=Home");
    expect(url).toContain("destination=Office");
    expect(url).toContain("travelmode=driving");
    const output = buildRouteYaml(
      {
        start: "Home",
        destination: "Office",
        mode: "driving",
        url,
        snapshot: "45 min\nHeavy traffic",
      },
      "2026-09-23T00:00:00.000Z",
    );
    expect(output).toContain("type: google_maps_route");
    expect(output).toContain('observed_at: "2026-09-23T00:00:00.000Z"');
    expect(output).toContain("Heavy traffic");
  });
});
