import {
  mkdirSync,
  openSync,
  closeSync,
  fsyncSync,
  renameSync,
  unlinkSync,
  existsSync,
  writeSync,
} from "node:fs";
import { dirname, join } from "node:path";

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
    return typeof parsed === "string" ? JSON.parse(parsed) : parsed;
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
    return typeof parsed === "string" ? JSON.parse(parsed) : parsed;
  }
}

type Pending = {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
};

export class McpClient {
  readonly baseUrl: string;
  readonly host: string;
  private controller = new AbortController();
  private reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
  private postUrl: string | undefined;
  private nextId = 0;
  private pending = new Map<number, Pending>();
  private endpointResolve: ((value: string) => void) | undefined;
  private endpointReject: ((error: Error) => void) | undefined;
  openliaTabOwned = false;

  constructor(baseUrl = "http://localhost:8931") {
    this.baseUrl = baseUrl.replace(/\/+$/, "") || "http://localhost:8931";
    this.host = hostHeader(this.baseUrl);
  }

  async connect(): Promise<void> {
    const response = await fetch(`${this.baseUrl}/sse`, {
      headers: { Accept: "text/event-stream", Host: this.host },
      signal: this.controller.signal,
    });
    if (!response.ok || !response.body)
      throw new Error(`MCP SSE connection failed: HTTP ${response.status}`);
    this.reader = response.body.getReader();
    void this.readSse();
    this.postUrl = await new Promise<string>((resolve, reject) => {
      this.endpointResolve = resolve;
      this.endpointReject = reject;
      setTimeout(
        () => reject(new Error("SSE stream closed before endpoint received")),
        10_000,
      );
    });
    await this.call(
      "initialize",
      {
        protocolVersion: "2024-11-05",
        capabilities: {},
        clientInfo: { name: "openlia-tools", version: "0.1.0" },
      },
      15_000,
    );
    await this.notify("notifications/initialized");
  }

  private async readSse(): Promise<void> {
    if (!this.reader) return;
    const decoder = new TextDecoder();
    let buffer = "";
    let event = "message";
    let data: string[] = [];
    const flush = (): void => {
      if (!data.length) return;
      const payload = data.join("\n");
      if (!this.postUrl && (event === "endpoint" || !payload.startsWith("{"))) {
        try {
          const url = new URL(payload, this.baseUrl).href;
          this.endpointResolve?.(url);
          this.endpointResolve = undefined;
          this.endpointReject = undefined;
        } catch {
          this.endpointReject?.(new Error("MCP endpoint event was invalid"));
        }
      } else {
        try {
          const parsed = JSON.parse(payload) as {
            id?: number;
            result?: unknown;
            error?: { message?: string };
          };
          if (typeof parsed.id === "number") {
            const pending = this.pending.get(parsed.id);
            if (pending) {
              this.pending.delete(parsed.id);
              clearTimeout(pending.timer);
              if (parsed.error)
                pending.reject(
                  new Error(parsed.error.message ?? "MCP request failed"),
                );
              else pending.resolve(parsed.result);
            }
          }
        } catch {
          // Ignore non-JSON server events; the browser relay may send comments or diagnostics.
        }
      }
      event = "message";
      data = [];
    };
    try {
      while (true) {
        const chunk = await this.reader.read();
        if (chunk.done) break;
        buffer += decoder.decode(chunk.value, { stream: true });
        const lines = buffer.split(/\r?\n/);
        buffer = lines.pop() ?? "";
        for (const line of lines) {
          if (!line) flush();
          else if (line.startsWith(":")) continue;
          else if (line.startsWith("event:")) event = line.slice(6).trim();
          else if (line.startsWith("data:"))
            data.push(line.slice(5).trimStart());
        }
      }
      flush();
      const error = new Error("MCP SSE stream closed");
      this.endpointReject?.(error);
      for (const pending of this.pending.values()) {
        clearTimeout(pending.timer);
        pending.reject(error);
      }
      this.pending.clear();
    } catch (error) {
      const failure = error instanceof Error ? error : new Error(String(error));
      this.endpointReject?.(failure);
      for (const pending of this.pending.values()) {
        clearTimeout(pending.timer);
        pending.reject(failure);
      }
      this.pending.clear();
    }
  }

  async call(
    method: string,
    params: Record<string, unknown>,
    timeout = 90_000,
  ): Promise<unknown> {
    if (!this.postUrl) throw new Error("MCP session is not connected");
    const id = ++this.nextId;
    const result = new Promise<unknown>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`MCP request timed out: ${method}`));
      }, timeout);
      this.pending.set(id, { resolve, reject, timer });
    });
    try {
      const response = await fetch(this.postUrl, {
        method: "POST",
        headers: { "Content-Type": "application/json", Host: this.host },
        body: JSON.stringify({ jsonrpc: "2.0", id, method, params }),
        signal: this.controller.signal,
      });
      if (!response.ok)
        throw new Error(`MCP request failed: HTTP ${response.status}`);
      const contentType = response.headers.get("content-type") ?? "";
      if (contentType.includes("application/json")) {
        const body = (await response.json()) as {
          id?: number;
          result?: unknown;
          error?: { message?: string };
        };
        const pending = this.pending.get(body.id ?? id);
        if (pending && body.id === id) {
          this.pending.delete(id);
          clearTimeout(pending.timer);
          if (body.error)
            pending.reject(
              new Error(body.error.message ?? "MCP request failed"),
            );
          else pending.resolve(body.result);
        }
      }
    } catch (error) {
      const pending = this.pending.get(id);
      if (pending) {
        this.pending.delete(id);
        clearTimeout(pending.timer);
        pending.reject(
          error instanceof Error ? error : new Error(String(error)),
        );
      }
      throw error;
    }
    return result;
  }

  async notify(
    method: string,
    params: Record<string, unknown> = {},
  ): Promise<void> {
    if (!this.postUrl) throw new Error("MCP session is not connected");
    const response = await fetch(this.postUrl, {
      method: "POST",
      headers: { "Content-Type": "application/json", Host: this.host },
      body: JSON.stringify({ jsonrpc: "2.0", method, params }),
      signal: this.controller.signal,
    });
    if (!response.ok)
      throw new Error(`MCP notification failed: HTTP ${response.status}`);
  }

  async callTool(
    name: string,
    argumentsValue: Record<string, unknown>,
    timeout = 90_000,
  ): Promise<ToolResult> {
    const result = await this.call(
      "tools/call",
      { name, arguments: argumentsValue },
      timeout,
    );
    if (!result || typeof result !== "object")
      throw new Error(`MCP tool ${name} returned no result`);
    return result as ToolResult;
  }

  getToolText(result: ToolResult): string {
    return resultText(result);
  }

  close(): void {
    this.controller.abort();
    void this.reader?.cancel();
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(new Error("MCP session closed"));
    }
    this.pending.clear();
  }
}

async function closeCurrentTab(client: McpClient): Promise<void> {
  try {
    await client.callTool("browser_tabs", { action: "close" }, 10_000);
  } catch {
    /* cleanup is best effort */
  }
}

function currentOwnedWelcomeTab(text: string): boolean {
  return text
    .split("\n")
    .some(
      (line) =>
        /\(current\)/i.test(line) &&
        /chrome-extension:\/\//i.test(line) &&
        /welcome/i.test(line),
    );
}

async function enforceTabCap(client: McpClient, maxTabs = 2): Promise<void> {
  try {
    const text = client.getToolText(
      await client.callTool("browser_tabs", { action: "list" }, 10_000),
    );
    const indexes = [...text.matchAll(/^\s*-\s*(\d+):/gm)]
      .map((match) => Number(match[1]))
      .filter(Number.isInteger)
      .sort((a, b) => b - a);
    for (const index of indexes.slice(maxTabs))
      await client
        .callTool("browser_tabs", { action: "close", index }, 5_000)
        .catch(() => undefined);
  } catch {
    /* tab cleanup must not bypass the privacy gate */
  }
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

function completionScript(
  platform: "chatgpt" | "gemini",
  prompt?: string,
): string {
  if (platform === "chatgpt")
    return `() => { const a=[...document.querySelectorAll("[data-message-author-role='assistant']")], last=a.at(-1); const send=document.querySelector("button[data-testid='send-button'],button[aria-label*='Send']"), stop=document.querySelector("button[data-testid='stop-button'],button[aria-label*='Stop'],[aria-label*='Stop generating']"); return JSON.stringify({assistant_count:a.length,last_assistant_text:last?.innerText?.trim()||"",send_present:!!send,stop_visible:!!stop}); }`;
  return `() => { const normalize=v=>(v||"").replace(/\\s+/g," ").trim(), expected=normalize(${JSON.stringify(prompt ?? "")}), users=[...document.querySelectorAll("user-query,.user-query-container,div[data-test-id='user-query']")].filter((e,i,a)=>!a.some((o,j)=>i!==j&&o.contains(e))), models=[...document.querySelectorAll("model-response,div[data-test-id='model-response'],message-content")].filter(e=>!e.closest("user-query,.user-query-container,div[data-test-id='user-query']")).filter(e=>normalize(e.innerText)!==expected), last=models.at(-1), stop=document.querySelector("button[aria-label*='Stop'],[aria-label*='Stop response']"); return JSON.stringify({user_count:users.length,model_count:models.length,last_model_text:last?.innerText?.trim()||"",stop_visible:!!stop}); }`;
}

async function evaluate(client: McpClient, script: string): Promise<unknown> {
  return parseEvaluate(
    client.getToolText(
      await client.callTool("browser_evaluate", { function: script }),
    ),
  );
}

async function waitForCompletion(
  client: McpClient,
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
  let owned = false;
  let lastError: unknown;
  try {
    for (let attempt = 0; attempt < 3; attempt++) {
      const candidate = new McpClient(mcpUrl || "http://localhost:8931");
      try {
        await candidate.connect();
        const tabs = candidate.getToolText(
          await candidate.callTool("browser_tabs", { action: "list" }, 15_000),
        );
        if (!currentOwnedWelcomeTab(tabs))
          throw new Error(
            "no current OpenLia-owned extension Welcome tab is available; no prompt was sent",
          );
        candidate.openliaTabOwned = true;
        owned = true;
        await candidate.callTool(
          "browser_navigate",
          {
            url:
              platform === "chatgpt"
                ? "https://chatgpt.com/?temporary-chat=true"
                : "https://gemini.google.com/app",
          },
          30_000,
        );
        client = candidate;
        break;
      } catch (error) {
        lastError = error;
        if (candidate.openliaTabOwned) await closeCurrentTab(candidate);
        candidate.close();
        owned = false;
        if (attempt === 2) throw error;
        await sleep(1000);
      }
    }
    if (!client) throw lastError ?? new Error("browser MCP preflight failed");
    await enforceTabCap(client);
    await sleep(1000);
    let snapshot = client.getToolText(
      await client.callTool("browser_snapshot", {}),
    );
    const active =
      platform === "chatgpt" ? activeChatGpt(snapshot) : activeGemini(snapshot);
    if (!active) {
      await client.callTool("browser_click", {
        target:
          platform === "chatgpt"
            ? 'button[data-testid="model-selector-dropdown"],button[aria-label*="Model"]'
            : 'button[aria-label="Temporary chat"]',
      });
      if (platform === "chatgpt")
        await client.callTool("browser_click", {
          target: 'button[role="switch"],div:has-text("Temporary chat")',
        });
      for (let attempt = 0; attempt < 6; attempt++) {
        await sleep(platform === "gemini" ? 1000 : 1500);
        snapshot = client.getToolText(
          await client.callTool("browser_snapshot", {}),
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
    const initial = (await evaluate(
      client,
      completionScript(platform, prompt),
    )) as Record<string, unknown>;
    const count = Number(
      initial[platform === "chatgpt" ? "assistant_count" : "model_count"] ?? 0,
    );
    await client.callTool("browser_type", {
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
    await client.callTool("browser_click", { target: send });
    await waitForCompletion(client, platform, count, prompt, timeout);
    const extraction =
      platform === "chatgpt"
        ? CHATGPT_EXTRACTION
        : GEMINI_EXTRACTION.replace(
            "__OPENLIA_PROMPT__",
            JSON.stringify(prompt),
          );
    const turns = parseEvaluate(
      client.getToolText(
        await client.callTool("browser_evaluate", { function: extraction }),
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
        await evaluate(client, "() => window.location.href"),
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
      if (owned) await closeCurrentTab(client);
      client.close();
    }
  }
}
