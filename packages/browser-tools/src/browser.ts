import {
  closeSync,
  existsSync,
  fsyncSync,
  mkdirSync,
  openSync,
  renameSync,
  unlinkSync,
  writeSync,
} from "node:fs";
import { dirname, join } from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import {
  StreamableHTTPClientTransport,
  StreamableHTTPError,
} from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import {
  ErrorCode,
  McpError,
  ResultSchema,
} from "@modelcontextprotocol/sdk/types.js";

export type BrowserMessage = {
  role: "user" | "assistant" | "system";
  content: string;
  turn?: number;
  references?: BrowserReference[];
};
export type BrowserReference = { title?: string; url: string };
export type BrowserJobMeta = {
  platform: "chatgpt" | "gemini";
  topic: string;
  model: string;
  url: string;
  started_at?: string;
};
export type BrowserRouteMeta = {
  start: string;
  destination: string;
  mode: string;
  url: string;
  snapshot: string;
  observed_at?: string;
};
export type ToolResult = {
  content?: Array<{ type?: string; text?: string }>;
  [key: string]: unknown;
};

const sleep = (milliseconds: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, milliseconds));

function hostHeader(baseUrl: string): string {
  const parsed = new URL(baseUrl);
  return `localhost:${parsed.port || "8931"}`;
}

function resultText(result: ToolResult): string {
  return (result.content ?? [])
    .filter((item) => item.type === "text" || item.type === undefined)
    .map((item) => item.text ?? "")
    .join("");
}

function parseEvaluate(raw: string): unknown {
  let cleaned = raw.trim();
  const marker = cleaned.indexOf("### Result");
  if (marker >= 0) {
    cleaned = cleaned.slice(marker + "### Result".length);
    const end = cleaned.indexOf("### Ran Playwright");
    if (end >= 0) cleaned = cleaned.slice(0, end);
    cleaned = cleaned.trim();
  }
  try {
    const parsed: unknown = JSON.parse(cleaned);
    if (typeof parsed !== "string") return parsed;
    try {
      return JSON.parse(parsed);
    } catch {
      return parsed;
    }
  } catch {
    const firstObject = Math.min(
      ...[cleaned.indexOf("{"), cleaned.indexOf("[")].filter(
        (value) => value >= 0,
      ),
    );
    if (!Number.isFinite(firstObject))
      throw new Error(
        `Could not parse evaluate result as JSON: ${raw.slice(0, 200)}`,
      );
    const lastObject = Math.max(
      cleaned.lastIndexOf("}"),
      cleaned.lastIndexOf("]"),
    );
    const parsed: unknown = JSON.parse(
      cleaned.slice(firstObject, lastObject + 1),
    );
    if (typeof parsed !== "string") return parsed;
    try {
      return JSON.parse(parsed);
    } catch {
      return parsed;
    }
  }
}

export class McpClient {
  readonly baseUrl: string;
  readonly host: string;
  private client: Client | undefined;
  private transport: StreamableHTTPClientTransport | undefined;
  private connectPromise: Promise<void> | undefined;
  private controller = new AbortController();
  private closed = false;
  private generation = 0;
  constructor(baseUrl = "http://localhost:8931/mcp") {
    const endpoint = new URL(baseUrl || "http://localhost:8931/mcp");
    if (endpoint.pathname === "/" || endpoint.pathname === "")
      endpoint.pathname = "/mcp";
    endpoint.pathname = endpoint.pathname.replace(/\/+$/, "") || "/mcp";
    this.baseUrl = endpoint.href.replace(/\/$/, "");
    this.host = hostHeader(this.baseUrl);
  }

  async connect(): Promise<void> {
    if (this.closed) throw new Error("MCP session is closed");
    if (this.client) return;
    if (!this.connectPromise) {
      const generation = ++this.generation;
      this.connectPromise = (async () => {
        const transport = new StreamableHTTPClientTransport(
          new URL(this.baseUrl),
          { requestInit: { headers: { Host: this.host } } },
        );
        const client = new Client(
          { name: "openlia-browser-tools", version: "0.1.0" },
          { capabilities: {} },
        );
        try {
          await client.connect(transport as Transport, {
            signal: this.controller.signal,
            timeout: 15_000,
          });
          if (this.closed || this.generation !== generation) {
            await client.close().catch(() => undefined);
            throw new Error("MCP connection was superseded");
          }
          this.transport = transport;
          this.client = client;
        } catch (error) {
          await transport.close().catch(() => undefined);
          throw error;
        }
      })().finally(() => {
        this.connectPromise = undefined;
      });
    }
    await this.connectPromise;
  }

  async call(
    method: string,
    params: Record<string, unknown>,
    timeout = 90_000,
    signal = this.controller.signal,
  ): Promise<unknown> {
    await this.connect();
    const client = this.client;
    const transport = this.transport;
    const generation = this.generation;
    if (!client || !transport) throw new Error("MCP connection failed");
    try {
      if (method === "tools/list")
        return await client.listTools(params, {
          signal,
          timeout,
        });
      if (method === "ping") return await client.ping({ signal, timeout });
      return await client.request({ method, params } as never, ResultSchema, {
        signal,
        timeout,
      });
    } catch (error) {
      if (this.isConnectionFailure(error, signal))
        this.invalidate(client, transport, generation);
      throw error;
    }
  }

  async notify(
    method: string,
    params: Record<string, unknown> = {},
  ): Promise<void> {
    await this.connect();
    const client = this.client;
    const transport = this.transport;
    const generation = this.generation;
    if (!client || !transport) throw new Error("MCP connection failed");
    try {
      await client.notification({ method, params } as never);
    } catch (error) {
      if (this.isConnectionFailure(error))
        this.invalidate(client, transport, generation);
      throw error;
    }
  }

  async callTool(
    name: string,
    argumentsValue: Record<string, unknown>,
    timeout = 90_000,
    signal = this.controller.signal,
  ): Promise<ToolResult> {
    await this.connect();
    const client = this.client;
    const transport = this.transport;
    const generation = this.generation;
    if (!client || !transport) throw new Error("MCP connection failed");
    let result: Awaited<ReturnType<Client["callTool"]>>;
    try {
      result = await client.callTool(
        { name, arguments: argumentsValue },
        undefined,
        { signal, timeout },
      );
    } catch (error) {
      if (this.isConnectionFailure(error, signal))
        this.invalidate(client, transport, generation);
      throw error;
    }
    if (!result || typeof result !== "object")
      throw new Error(`MCP tool ${name} returned no result`);
    return result as unknown as ToolResult;
  }

  getToolText(result: ToolResult): string {
    return resultText(result);
  }

  close(): void {
    this.closed = true;
    this.generation += 1;
    this.controller.abort();
    const client = this.client;
    const transport = this.transport;
    this.client = undefined;
    this.transport = undefined;
    void (async () => {
      await transport?.terminateSession().catch(() => undefined);
      await client?.close().catch(() => undefined);
    })();
  }

  private invalidate(
    client: Client,
    transport: StreamableHTTPClientTransport,
    generation: number,
  ): void {
    if (this.closed || this.generation !== generation || this.client !== client)
      return;
    this.generation += 1;
    this.controller.abort();
    this.client = undefined;
    this.transport = undefined;
    this.controller = new AbortController();
    void (async () => {
      await transport.terminateSession().catch(() => undefined);
      await client
        .close()
        .catch(() => transport.close().catch(() => undefined));
    })();
  }

  private isConnectionFailure(error: unknown, signal?: AbortSignal): boolean {
    if (signal?.aborted) return false;
    if (error instanceof StreamableHTTPError) return true;
    if (error instanceof McpError)
      return (
        error.code === ErrorCode.ConnectionClosed &&
        !/cancel/i.test(error.message)
      );
    if (error instanceof TypeError) return true;
    const code =
      error && typeof error === "object"
        ? String(
            (error as { code?: unknown; cause?: { code?: unknown } }).code ??
              (error as { cause?: { code?: unknown } }).cause?.code ??
              "",
          )
        : "";
    return new Set([
      "ECONNABORTED",
      "ECONNREFUSED",
      "ECONNRESET",
      "EHOSTUNREACH",
      "ENETUNREACH",
      "EPIPE",
    ]).has(code);
  }
}

type TabEntry = { index: number; current: boolean; label: string };
export type OwnedTab = { index: number; created: boolean };

function tabEntries(text: string): TabEntry[] {
  return [...text.matchAll(/^\s*-\s*(\d+):([^\n]*)$/gm)].map((match) => ({
    index: Number(match[1]),
    current: /\(current\)/i.test(match[2] ?? ""),
    label: match[2] ?? "",
  }));
}

export function currentTabIndex(text: string): number | null {
  const entry = tabEntries(text).find((tab) => tab.current);
  return entry?.index ?? null;
}

async function listTabs(client: McpClient): Promise<TabEntry[]> {
  return tabEntries(
    client.getToolText(
      await client.callTool("browser_tabs", { action: "list" }, 15_000),
    ),
  );
}

async function selectTab(client: McpClient, index: number): Promise<void> {
  await client.callTool("browser_tabs", { action: "select", index }, 10_000);
}

export async function openOwnedTab(
  client: McpClient,
  url: string,
): Promise<OwnedTab> {
  const before = await listTabs(client);
  if (!before.length) throw new Error("browser MCP returned no tabs");
  const current = before.find((tab) => tab.current);
  if (current && /chrome-extension:\/\/.*welcome/i.test(current.label)) {
    await callOwnedTool(
      client,
      current.index,
      "browser_navigate",
      { url },
      60_000,
    );
    return { index: current.index, created: false };
  }
  try {
    await client.callTool("browser_tabs", { action: "new" }, 15_000);
    const after = await listTabs(client);
    const created = after.find((tab) => tab.current);
    if (!created) throw new Error("browser MCP did not identify the new tab");
    await callOwnedTool(
      client,
      created.index,
      "browser_navigate",
      { url },
      60_000,
    );
    return { index: created.index, created: true };
  } catch (error) {
    if (!current) throw error;
    try {
      await callOwnedTool(
        client,
        current.index,
        "browser_navigate",
        { url },
        60_000,
      );
    } catch (fallbackError) {
      throw new AggregateError(
        [error, fallbackError],
        `browser tab creation and current-tab fallback both failed: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
    return { index: current.index, created: false };
  }
}

async function closeOwnedTab(
  client: McpClient,
  tab: OwnedTab | undefined,
  expectedLabel = "",
): Promise<void> {
  if (!tab) return;
  try {
    if (!tab.created) {
      await callOwnedTool(
        client,
        tab.index,
        "browser_navigate",
        {
          url: "about:blank",
        },
        30_000,
      );
      return;
    }
    if (expectedLabel) {
      const entry = (await listTabs(client)).find(
        (entry) => entry.index === tab.index,
      );
      if (!entry?.label.toLowerCase().includes(expectedLabel)) return;
    }
    await client.callTool(
      "browser_tabs",
      { action: "close", index: tab.index },
      10_000,
    );
  } catch {
    /* cleanup is best effort */
  }
}

async function callOwnedTool(
  client: McpClient,
  tabIndex: number,
  name: string,
  argumentsValue: Record<string, unknown>,
  timeout = 90_000,
): Promise<ToolResult> {
  await selectTab(client, tabIndex);
  return client.callTool(name, argumentsValue, timeout);
}

function activeChatGpt(text: string): boolean {
  const value = text.toLowerCase();
  return (
    value.includes("temporary chat") &&
    (value.includes("history") ||
      value.includes("training") ||
      value.includes("not appear"))
  );
}

function activeGemini(text: string): boolean {
  const value = text.toLowerCase();
  return (
    value.includes("just stopping by") ||
    (value.includes("temporary chat") && value.includes("won't be saved")) ||
    value.includes("will not be saved")
  );
}

function expectedChatHost(platform: "chatgpt" | "gemini"): string {
  return platform === "chatgpt" ? "chatgpt.com" : "gemini.google.com";
}

async function assertChatPage(
  client: McpClient,
  tabIndex: number,
  platform: "chatgpt" | "gemini",
): Promise<void> {
  const url = String(
    await evaluate(client, tabIndex, "() => window.location.href"),
  );
  if (!url.toLowerCase().includes(expectedChatHost(platform)))
    throw new Error(`${platform} owned tab changed before prompt submission`);
}

function completionScript(
  platform: "chatgpt" | "gemini",
  prompt?: string,
): string {
  if (platform === "chatgpt")
    return `() => { const a=[...document.querySelectorAll("[data-message-author-role='assistant']")], last=a.at(-1); const send=document.querySelector("button[data-testid='send-button'],button[aria-label*='Send']"), stop=document.querySelector("button[data-testid='stop-button'],button[aria-label*='Stop'],[aria-label*='Stop generating']"); return JSON.stringify({assistant_count:a.length,last_assistant_text:last?.innerText?.trim()||"",send_present:!!send,stop_visible:!!stop}); }`;
  return `() => { const normalize=v=>(v||"").replace(/\\s+/g," ").trim(), expected=normalize(${JSON.stringify(prompt ?? "")}), users=[...document.querySelectorAll("user-query,.user-query-container,div[data-test-id='user-query']")].filter((e,i,a)=>!a.some((o,j)=>i!==j&&o.contains(e))), models=[...document.querySelectorAll("model-response,div[data-test-id='model-response'],message-content")].filter(e=>!e.closest("user-query,.user-query-container,div[data-test-id='user-query']")).filter(e=>normalize(e.innerText)!==expected), last=models.at(-1), stop=document.querySelector("button[aria-label*='Stop'],[aria-label*='Stop response']"); return JSON.stringify({user_count:users.length,model_count:models.length,last_model_text:last?.innerText?.trim()||"",stop_visible:!!stop}); }`;
}

async function evaluate(
  client: McpClient,
  tabIndex: number,
  script: string,
): Promise<unknown> {
  return parseEvaluate(
    client.getToolText(
      await callOwnedTool(client, tabIndex, "browser_evaluate", {
        function: script,
      }),
    ),
  );
}

async function waitForCompletion(
  client: McpClient,
  tabIndex: number,
  platform: "chatgpt" | "gemini",
  initial: number,
  prompt: string,
  timeoutSeconds: number,
): Promise<void> {
  const started = Date.now();
  let previous = "";
  let stable = 0;
  while (Date.now() - started < timeoutSeconds * 1000) {
    const state = (await evaluate(
      client,
      tabIndex,
      completionScript(platform, prompt),
    )) as Record<string, unknown>;
    const count = Number(
      state[platform === "chatgpt" ? "assistant_count" : "model_count"] ?? 0,
    );
    const text = String(
      state[
        platform === "chatgpt" ? "last_assistant_text" : "last_model_text"
      ] ?? "",
    ).trim();
    const ready =
      count > initial &&
      text.length > 0 &&
      !state.stop_visible &&
      (platform === "gemini" || Boolean(state.send_present));
    if (ready) {
      stable = text === previous ? stable + 1 : 0;
      previous = text;
      if (stable >= 2) return;
    } else {
      stable = 0;
      previous = "";
    }
    await sleep(1500);
  }
  throw new Error(
    `${platform} generation did not reach a stable completed response within ${timeoutSeconds}s`,
  );
}

const CHATGPT_EXTRACTION = `async () => { const turns=[], clipboard=navigator.clipboard, original=clipboard?.writeText; let copied=null; if(clipboard&&original) clipboard.writeText=t=>(copied=t,Promise.resolve()); try { for(const [idx,block] of [...document.querySelectorAll("[data-message-author-role]")].entries()){ const role=block.getAttribute("data-message-author-role"); if(role==="user") turns.push({role:"user",turn:Math.floor(idx/2)+1,content:(block.innerText||"").trim(),references:[]}); else { const container=block.closest("[data-testid^='conversation-turn-']")||block.parentElement, button=container?.querySelector("button[data-testid='copy-turn-action-button'],button[aria-label*='Copy']"); copied=null; button?.click(); await new Promise(r=>setTimeout(r,250)); turns.push({role:"assistant",turn:Math.floor(idx/2)+1,content:(copied||(block.innerText||"")).trim(),references:[...block.querySelectorAll("a[href]")].map(a=>({title:(a.innerText||a.getAttribute("aria-label")||"").trim(),url:a.href})).filter(x=>x.url.startsWith("http")&&!x.url.includes("chatgpt.com/c/"))}); } } return JSON.stringify(turns); } finally { if(clipboard&&original) clipboard.writeText=original; } }`;
const GEMINI_EXTRACTION = `(async()=>{const normalize=v=>(v||"").replace(/\\s+/g," ").trim(),userSelector="user-query,.user-query-container,div[data-test-id='user-query']",users=[...document.querySelectorAll(userSelector)].filter((e,i,a)=>!a.some((o,j)=>i!==j&&o.contains(e))),models=[...document.querySelectorAll("model-response,div[data-test-id='model-response'],message-content")].filter(e=>!e.closest(userSelector)),entries=[...users.map(node=>({role:"user",node})),...models.map(node=>({role:"assistant",node}))].sort((a,b)=>a.node.compareDocumentPosition(b.node)&Node.DOCUMENT_POSITION_FOLLOWING?-1:1),turns=[]; for(const e of entries){const text=(e.node.innerText||"").trim().replace(/^(You said|Gemini said)\\s*/i,""); if(text) turns.push({role:e.role,content:text,references:[...e.node.querySelectorAll("a[href]")].map(a=>({title:(a.innerText||a.getAttribute("aria-label")||"").trim(),url:a.href})).filter(x=>x.url.startsWith("http")&&!x.url.includes("gemini.google.com/app"))})} if(!turns.some(x=>x.role==="user"))turns.unshift({role:"user",content:__OPENLIA_PROMPT__,references:[]}); turns.forEach((x,i)=>x.turn=Math.floor(i/2)+1); return JSON.stringify(turns)})()`;

export function cleanUrl(value: string): string {
  try {
    let url = new URL(value);
    if (
      (url.hostname === "google.com" || url.hostname.endsWith(".google.com")) &&
      url.pathname === "/url"
    ) {
      const target = url.searchParams.get("q") ?? url.searchParams.get("url");
      if (target) url = new URL(target);
    }
    if (url.protocol !== "http:" && url.protocol !== "https:") return "";
    for (const key of [...url.searchParams.keys()])
      if (/^(utm_|gclid$|fbclid$|ved$|usg$|sa$|ref$|source$)/i.test(key))
        url.searchParams.delete(key);
    url.hash = "";
    return url.href;
  } catch {
    return "";
  }
}

export function mapsRouteUrl(
  start: string,
  destination: string,
  mode: string,
): string {
  const params = new URLSearchParams({
    api: "1",
    origin: start,
    destination,
    travelmode: mode,
  });
  return `https://www.google.com/maps/dir/?${params.toString()}`;
}

export function buildRouteYaml(
  meta: BrowserRouteMeta,
  now = new Date().toISOString(),
): string {
  return `${yaml({
    schema: 1,
    type: "google_maps_route",
    request: {
      start: meta.start,
      destination: meta.destination,
      mode: meta.mode,
    },
    session: {
      observed_at: meta.observed_at ?? now,
      url: meta.url,
    },
    snapshot: meta.snapshot,
  })}\n`;
}

function siteMetadata(urlValue: string, title: string): [string, string] {
  const domain = new URL(urlValue).hostname.toLowerCase().replace(/^www\./, "");
  if (domain === "github.com" || domain.endsWith(".github.com"))
    return [domain, "GitHub"];
  if (domain === "stackoverflow.com") return [domain, "Stack Overflow"];
  if (domain === "wikipedia.org" || domain.endsWith(".wikipedia.org"))
    return [domain, "Wikipedia"];
  if (domain === "docs.rs") return [domain, "docs.rs"];
  if (domain === "go.dev" || domain.endsWith(".go.dev")) return [domain, "Go"];
  if (domain === "arxiv.org") return [domain, "arXiv"];
  return [domain, title || domain];
}

function cleanTitle(value: string, urlValue: string): string {
  const title = value.replace(/\s+/g, " ").trim();
  return title || new URL(urlValue).hostname;
}

function yamlScalar(value: unknown): string {
  if (value === null) return "null";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") return String(value);
  const string = String(value);
  if (string.includes("\n")) return "|";
  if (
    string === "" ||
    /[:#\-?{},[\]&*!|>'"%@`\n\r\t]/.test(string) ||
    /^(true|false|null|~|[-+]?\d+(\.\d+)?)$/i.test(string)
  )
    return JSON.stringify(string);
  return string;
}

function yaml(value: unknown, indent = 0): string {
  const pad = " ".repeat(indent);
  if (Array.isArray(value))
    return value
      .map((item) => {
        if (item && typeof item === "object" && !Array.isArray(item)) {
          const lines = yaml(item, indent + 2).split("\n");
          return `${pad}- ${lines[0]?.trimStart() ?? ""}${lines.slice(1).length ? `\n${lines.slice(1).join("\n")}` : ""}`;
        }
        return `${pad}- ${yamlScalar(item)}`;
      })
      .join("\n");
  if (value && typeof value === "object")
    return Object.entries(value)
      .map(([key, item]) => {
        if (typeof item === "string" && item.includes("\n"))
          return `${pad}${key}: |\n${item
            .split("\n")
            .map((line) => `${" ".repeat(indent + 2)}${line}`)
            .join("\n")}`;
        if (item && typeof item === "object")
          return `${pad}${key}:\n${yaml(item, indent + 2)}`;
        return `${pad}${key}: ${yamlScalar(item)}`;
      })
      .join("\n");
  return `${pad}${yamlScalar(value)}`;
}

export function buildConversationYaml(
  meta: BrowserJobMeta,
  messages: BrowserMessage[],
  now = new Date().toISOString(),
): string {
  const references = new Map<
    string,
    {
      id: string;
      index: number;
      title: string;
      url: string;
      domain: string;
      site_name: string;
      turns: Set<number>;
    }
  >();
  for (const [index, message] of messages.entries()) {
    for (const raw of message.references ?? []) {
      const url = cleanUrl(raw.url);
      if (!url) continue;
      const turn = message.turn ?? Math.floor(index / 2) + 1;
      const current = references.get(url);
      if (current) current.turns.add(turn);
      else {
        const [domain, siteName] = siteMetadata(url, raw.title ?? "");
        references.set(url, {
          id: `ref-${references.size + 1}`,
          index: references.size + 1,
          title: cleanTitle(raw.title ?? "", url),
          url,
          domain,
          site_name: siteName,
          turns: new Set([turn]),
        });
      }
    }
  }
  const formatted = messages.map((message, index) => {
    const role = message.role;
    const item: Record<string, unknown> = {
      role,
      turn: message.turn ?? Math.floor(index / 2) + 1,
      content: message.content.trim(),
    };
    const seen = new Set<string>();
    const refs: Array<Record<string, unknown>> = [];
    for (const raw of message.references ?? []) {
      const ref = references.get(cleanUrl(raw.url));
      if (!ref || seen.has(ref.url)) continue;
      seen.add(ref.url);
      refs.push({
        id: ref.id,
        index: ref.index,
        title: ref.title,
        url: ref.url,
        domain: ref.domain,
        site_name: ref.site_name,
      });
    }
    if (refs.length) item.references = refs;
    return item;
  });
  const session = {
    platform: meta.platform,
    mode: "temporary",
    started_at: meta.started_at ?? now,
    model: meta.model,
    topic: meta.topic,
    url: cleanUrl(meta.url),
    turns_count: messages.length,
  };
  const document: Record<string, unknown> = {
    schema: 1,
    session,
    messages: formatted,
  };
  if (references.size)
    document.references = [...references.values()].map((ref) => ({
      id: ref.id,
      index: ref.index,
      title: ref.title,
      url: ref.url,
      domain: ref.domain,
      site_name: ref.site_name,
      turns: [...ref.turns].sort((a, b) => a - b),
    }));
  return `${yaml(document)}\n`;
}

function atomicWrite(path: string, contents: string): void {
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  const temporary = `${path}.tmp-${crypto.randomUUID()}`;
  const descriptor = openSync(temporary, "wx", 0o600);
  try {
    const bytes = new TextEncoder().encode(contents);
    writeSync(descriptor, bytes);
    fsyncSync(descriptor);
  } finally {
    closeSync(descriptor);
  }
  try {
    renameSync(temporary, path);
  } catch (error) {
    if (existsSync(temporary)) unlinkSync(temporary);
    throw error;
  }
}

export async function executeBrowserChat(
  platform: "chatgpt" | "gemini",
  prompt: string,
  topic: string,
  outputPath: string,
  mcpUrl: string,
  timeout: number,
  dataRoot: string,
): Promise<string> {
  let client: McpClient | undefined;
  let ownedTab: OwnedTab | undefined;
  let lastError: unknown;
  try {
    for (let attempt = 0; attempt < 3; attempt++) {
      const candidate = new McpClient(mcpUrl || "http://localhost:8931");
      let candidateTab: OwnedTab | undefined;
      try {
        await candidate.connect();
        candidateTab = await openOwnedTab(
          candidate,
          platform === "chatgpt"
            ? "https://chatgpt.com/?temporary-chat=true"
            : "https://gemini.google.com/app",
        );
        client = candidate;
        ownedTab = candidateTab;
        break;
      } catch (error) {
        lastError = error;
        await closeOwnedTab(
          candidate,
          candidateTab,
          platform === "chatgpt" ? "chatgpt.com" : "gemini.google.com",
        );
        candidate.close();
        ownedTab = undefined;
        if (attempt === 2) throw error;
        await sleep(1000);
      }
    }
    if (!client) throw lastError ?? new Error("browser MCP preflight failed");
    if (!ownedTab) throw new Error("browser MCP did not return an owned tab");
    const tabIndex = ownedTab.index;
    await sleep(1000);
    let snapshot = client.getToolText(
      await callOwnedTool(client, tabIndex, "browser_snapshot", {}),
    );
    const active =
      platform === "chatgpt" ? activeChatGpt(snapshot) : activeGemini(snapshot);
    if (!active) {
      await callOwnedTool(client, tabIndex, "browser_click", {
        target:
          platform === "chatgpt"
            ? 'button[data-testid="model-selector-dropdown"],button[aria-label*="Model"]'
            : 'button[aria-label="Temporary chat"]',
      });
      if (platform === "chatgpt")
        await callOwnedTool(client, tabIndex, "browser_click", {
          target: 'button[role="switch"],div:has-text("Temporary chat")',
        });
      for (let attempt = 0; attempt < 6; attempt++) {
        await sleep(platform === "gemini" ? 1000 : 1500);
        snapshot = client.getToolText(
          await callOwnedTool(client, tabIndex, "browser_snapshot", {}),
        );
        if (
          platform === "chatgpt"
            ? activeChatGpt(snapshot)
            : activeGemini(snapshot)
        )
          break;
        if (attempt === 5)
          throw new Error(
            `Zero-Tolerance Gatekeeper: ${platform} Temporary Chat mode could not be verified. Aborting query.`,
          );
      }
    }
    await assertChatPage(client, tabIndex, platform);
    const verifiedSnapshot = client.getToolText(
      await callOwnedTool(client, tabIndex, "browser_snapshot", {}),
    );
    if (
      !(platform === "chatgpt"
        ? activeChatGpt(verifiedSnapshot)
        : activeGemini(verifiedSnapshot))
    )
      throw new Error(
        `Zero-Tolerance Gatekeeper: ${platform} Temporary Chat changed before prompt submission. Aborting query.`,
      );
    const initial = (await evaluate(
      client,
      tabIndex,
      completionScript(platform, prompt),
    )) as Record<string, unknown>;
    const count = Number(
      initial[platform === "chatgpt" ? "assistant_count" : "model_count"] ?? 0,
    );
    await callOwnedTool(client, tabIndex, "browser_type", {
      target:
        platform === "chatgpt"
          ? '#prompt-textarea,div[contenteditable="true"]'
          : 'div.ql-editor[contenteditable="true"]',
      text: prompt,
    });
    await sleep(500);
    const send =
      platform === "chatgpt"
        ? 'button[data-testid="send-button"],button[aria-label*="Send"]'
        : 'button[aria-label="Send message"]';
    await callOwnedTool(client, tabIndex, "browser_click", {
      target: send,
    });
    await waitForCompletion(client, tabIndex, platform, count, prompt, timeout);
    const extraction =
      platform === "chatgpt"
        ? CHATGPT_EXTRACTION
        : GEMINI_EXTRACTION.replace(
            "__OPENLIA_PROMPT__",
            JSON.stringify(prompt),
          );
    const turns = parseEvaluate(
      client.getToolText(
        await callOwnedTool(client, tabIndex, "browser_evaluate", {
          function: extraction,
        }),
      ),
    ) as BrowserMessage[];
    if (
      !Array.isArray(turns) ||
      !turns.length ||
      turns.at(-1)?.role !== "assistant" ||
      !turns.at(-1)?.content.trim()
    )
      throw new Error(
        `${platform} response was incomplete; no non-empty assistant turn was extracted`,
      );
    if (platform === "gemini") {
      const currentUrl = String(
        await evaluate(client, tabIndex, "() => window.location.href"),
      );
      if (/\/app\/[A-Za-z0-9_-]{10,}/.test(currentUrl))
        throw new Error(
          "Gatekeeper Leak Detected: Gemini persisted chat into URL",
        );
    }
    const platformRoot = join(dataRoot, "workspace", "knowledge", platform);
    const filename = `${new Date().toISOString().slice(0, 19).replaceAll("-", "").replace("T", "_").replaceAll(":", "")}_${
      (topic || prompt.slice(0, 40))
        .trim()
        .toLowerCase()
        .replace(/[^A-Za-z0-9_-]+/g, "_")
        .replace(/^_+|_+$/g, "")
        .slice(0, 50) || "chat"
    }.yaml`;
    const destination = outputPath || join(platformRoot, filename);
    atomicWrite(
      destination,
      buildConversationYaml(
        {
          platform,
          topic: topic || prompt.slice(0, 40),
          model: platform === "chatgpt" ? "ChatGPT" : "Gemini",
          url:
            platform === "chatgpt"
              ? "https://chatgpt.com/?temporary-chat=true"
              : "https://gemini.google.com/app",
        },
        turns,
      ),
    );
    return destination;
  } finally {
    if (client) {
      await closeOwnedTab(
        client,
        ownedTab,
        platform === "chatgpt" ? "chatgpt.com" : "gemini.google.com",
      );
      client.close();
    }
  }
}

export async function executeBrowserRoute(
  start: string,
  destination: string,
  mode: string,
  outputPath: string,
  mcpUrl: string,
  timeout: number,
): Promise<string> {
  const client = new McpClient(mcpUrl || "http://localhost:8931");
  let ownedTab: OwnedTab | undefined;
  try {
    await client.connect();
    ownedTab = await openOwnedTab(
      client,
      mapsRouteUrl(start, destination, mode),
    );
    await sleep(Math.min(5_000, Math.max(1_000, timeout * 100)));
    if (!ownedTab) throw new Error("browser MCP did not return an owned tab");
    const tabIndex = ownedTab.index;
    const snapshot = client.getToolText(
      await callOwnedTool(client, tabIndex, "browser_snapshot", {}),
    );
    if (!snapshot.trim())
      throw new Error("Google Maps returned an empty route snapshot");
    const currentUrl = String(
      await evaluate(client, tabIndex, "() => window.location.href"),
    );
    atomicWrite(
      outputPath,
      buildRouteYaml({
        start,
        destination,
        mode,
        url: currentUrl || mapsRouteUrl(start, destination, mode),
        snapshot,
      }),
    );
    return outputPath;
  } finally {
    await closeOwnedTab(client, ownedTab, "google.com/maps");
    client.close();
  }
}
