import { DatabaseSync } from "node:sqlite";
import { createHash, randomUUID, timingSafeEqual } from "node:crypto";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdirSync, readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { basename, resolve } from "node:path";
import {
  createServer,
  type IncomingMessage,
  type ServerResponse,
} from "node:http";
import { executeBrowserChat, executeBrowserRoute, McpClient } from "./browser";

const bind = process.env.BROWSER_TOOLS_BIND ?? "127.0.0.1";
const port = Number(process.env.BROWSER_TOOLS_PORT ?? "8932");
const mcpUrl = (
  process.env.BROWSER_TOOLS_MCP_URL ?? "http://127.0.0.1:8931"
).replace(/\/+$/, "");
const dataRoot = resolve(
  process.env.BROWSER_TOOLS_DATA_ROOT ??
    resolve(homedir(), "services", "browser-tools", "data"),
);
const databasePath = resolve(dataRoot, "jobs.sqlite3");
const tokenFile = process.env.BROWSER_TOOLS_TOKEN_FILE ?? "";
const configuredToken = tokenFile ? readFileSync(tokenFile, "utf8").trim() : "";
const requireAuth = process.env.BROWSER_TOOLS_REQUIRE_AUTH === "1";
const supervisePlaywright =
  process.env.BROWSER_TOOLS_SUPERVISE_PLAYWRIGHT === "1";
const playwrightPort = Number(
  process.env.BROWSER_TOOLS_PLAYWRIGHT_PORT ?? "8931",
);
const playwrightTokenFile =
  process.env.BROWSER_TOOLS_PLAYWRIGHT_TOKEN_FILE ?? "";
const maxBodyBytes = 1024 * 1024;
const routeModes = new Set(["driving", "transit", "walking", "bicycling"]);
const tools = new Set(["chatgpt-chat", "gemini-chat", "maps-route"]);

type JsonObject = Record<string, unknown>;
type JobStatus =
  | "queued"
  | "running"
  | "blocked_browser_offline"
  | "completed"
  | "failed"
  | "failed_uncertain"
  | "cancelled";
type JobRow = {
  id: string;
  client_id: string;
  tool: string;
  prompt: string;
  topic: string;
  input_json: string;
  output_path: string;
  fingerprint: string;
  idempotency_key: string;
  timeout_seconds: number;
  status: JobStatus;
  phase: string;
  error: string;
  created_at: string;
  updated_at: string;
};

function timestamp(): string {
  return new Date().toISOString();
}

function redact(value: string): string {
  return value
    .replace(/bearer\s+[A-Za-z0-9._~+/=-]+/gi, "Bearer [REDACTED]")
    .replace(
      /([A-Z][A-Z0-9_]*(?:KEY|TOKEN|SECRET|PASSWORD)[A-Z0-9_]*)\s*=\s*[^\s,;]+/gi,
      "$1=[REDACTED]",
    );
}

function safeSlug(value: string): string {
  return (
    value
      .trim()
      .toLowerCase()
      .replace(/[^A-Za-z0-9_-]+/g, "_")
      .replace(/^_+|_+$/g, "")
      .slice(0, 50) || "browser-job"
  );
}

function fingerprint(tool: string, input: string, topic: string): string {
  return createHash("sha256")
    .update(`${tool}\0${input}\0${topic}`)
    .digest("hex");
}

function publicJob(row: JobRow | null): JsonObject {
  if (!row) return { ok: false, error: "job not found" };
  const result: JsonObject = {
    ok: true,
    job_id: row.id,
    client_id: row.client_id,
    tool: row.tool,
    status: row.status,
    phase: row.phase,
    created_at: row.created_at,
    updated_at: row.updated_at,
    result_name: basename(row.output_path),
  };
  if (row.error) result.error = row.error;
  return result;
}

function constantTimeEqual(left: string, right: string): boolean {
  const a = Buffer.from(left);
  const b = Buffer.from(right);
  return a.length === b.length && timingSafeEqual(a, b);
}

function authorized(request: IncomingMessage): boolean {
  if (!requireAuth) return true;
  const header = request.headers.authorization ?? "";
  return (
    header.startsWith("Bearer ") &&
    constantTimeEqual(header.slice("Bearer ".length), configuredToken)
  );
}

function json(
  response: ServerResponse,
  payload: JsonObject,
  status = 200,
): void {
  const body = JSON.stringify(payload);
  response.writeHead(status, {
    "Content-Type": "application/json",
    "Cache-Control": "no-store",
    "Content-Length": Buffer.byteLength(body),
  });
  response.end(body);
}

function bytes(
  response: ServerResponse,
  body: Buffer,
  type: string,
  status = 200,
): void {
  response.writeHead(status, {
    "Content-Type": type,
    "Cache-Control": "no-store",
    "Content-Length": body.length,
  });
  response.end(body);
}

async function body(request: IncomingMessage): Promise<JsonObject | null> {
  const chunks: Buffer[] = [];
  let total = 0;
  for await (const chunk of request) {
    const value = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    total += value.length;
    if (total > maxBodyBytes) return null;
    chunks.push(value);
  }
  try {
    const parsed: unknown = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as JsonObject)
      : null;
  } catch {
    return null;
  }
}

class Store {
  readonly db: DatabaseSync;
  constructor() {
    mkdirSync(dataRoot, { recursive: true, mode: 0o700 });
    this.db = new DatabaseSync(databasePath);
    this.db.exec("PRAGMA journal_mode=WAL");
    this.db.exec(`CREATE TABLE IF NOT EXISTS jobs (
      id TEXT PRIMARY KEY,
      client_id TEXT NOT NULL,
      tool TEXT NOT NULL,
      prompt TEXT NOT NULL,
      topic TEXT NOT NULL,
      input_json TEXT NOT NULL,
      output_path TEXT NOT NULL,
      fingerprint TEXT NOT NULL,
      idempotency_key TEXT NOT NULL,
      timeout_seconds INTEGER NOT NULL,
      status TEXT NOT NULL,
      phase TEXT NOT NULL,
      error TEXT NOT NULL,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL
    )`);
    this.db
      .prepare(
        "UPDATE jobs SET status='failed_uncertain',phase='daemon_restart',error=?,updated_at=? WHERE status IN ('running','blocked_browser_offline')",
      )
      .run("browser-tools restarted while job was active", timestamp());
  }
  get(id: string): JobRow | null {
    return (
      (this.db.prepare("SELECT * FROM jobs WHERE id=?").get(id) as
        | JobRow
        | undefined) ?? null
    );
  }
  queued(): JobRow[] {
    return this.db
      .prepare("SELECT * FROM jobs WHERE status='queued' ORDER BY created_at")
      .all() as JobRow[];
  }
  activeDuplicate(
    clientId: string,
    fingerprintValue: string,
    key: string,
  ): JobRow | null {
    const query = key
      ? this.db.prepare(
          "SELECT * FROM jobs WHERE client_id=? AND idempotency_key=? AND status IN ('queued','running','blocked_browser_offline') ORDER BY created_at DESC LIMIT 1",
        )
      : this.db.prepare(
          "SELECT * FROM jobs WHERE client_id=? AND fingerprint=? AND status IN ('queued','running','blocked_browser_offline') ORDER BY created_at DESC LIMIT 1",
        );
    return (
      (query.get(clientId, key || fingerprintValue) as JobRow | undefined) ??
      null
    );
  }
  create(row: Omit<JobRow, "status" | "phase" | "error" | "updated_at">): void {
    this.db
      .prepare(`INSERT INTO jobs
      (id,client_id,tool,prompt,topic,input_json,output_path,fingerprint,idempotency_key,timeout_seconds,status,phase,error,created_at,updated_at)
      VALUES (?,?,?,?,?,?,?,?,?,?,'queued','queued','',?,?)`)
      .run(
        row.id,
        row.client_id,
        row.tool,
        row.prompt,
        row.topic,
        row.input_json,
        row.output_path,
        row.fingerprint,
        row.idempotency_key,
        row.timeout_seconds,
        row.created_at,
        row.created_at,
      );
  }
  update(id: string, status: JobStatus, phase: string, error = ""): void {
    this.db
      .prepare(
        "UPDATE jobs SET status=?,phase=?,error=?,updated_at=? WHERE id=?",
      )
      .run(status, phase, redact(error).slice(0, 2000), timestamp(), id);
  }
}

class Arbiter {
  private holder: { owner: string; release: () => void } | undefined;
  private waiting: Array<{
    owner: string;
    resolve: (release: () => void) => void;
    reject: (error: Error) => void;
    timer: ReturnType<typeof setTimeout>;
  }> = [];
  async acquire(owner: string, timeoutMs = 600_000): Promise<() => void> {
    if (!this.holder) return this.grant(owner);
    if (this.holder.owner === owner) return () => undefined;
    return new Promise<() => void>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waiting = this.waiting.filter((item) => item.resolve !== resolve);
        reject(new Error("browser lease acquisition timed out"));
      }, timeoutMs);
      this.waiting.push({ owner, resolve, reject, timer });
    });
  }
  private grant(owner: string): () => void {
    let released = false;
    const release = (): void => {
      if (released || this.holder?.owner !== owner) return;
      released = true;
      this.holder = undefined;
      const next = this.waiting.shift();
      if (next) {
        clearTimeout(next.timer);
        next.resolve(this.grant(next.owner));
      }
    };
    this.holder = { owner, release };
    return release;
  }
  status(): JsonObject {
    return {
      busy: Boolean(this.holder),
      owner: this.holder?.owner ?? null,
      waiting: this.waiting.length,
    };
  }
}

class JobService {
  readonly store = new Store();
  readonly arbiter = new Arbiter();
  readonly queue: string[] = [];
  pumping = false;
  constructor() {
    this.queue.push(...this.store.queued().map((row) => row.id));
    void this.pump();
  }
  enqueue(id: string): void {
    this.queue.push(id);
    void this.pump();
  }
  async pump(): Promise<void> {
    if (this.pumping) return;
    this.pumping = true;
    try {
      while (this.queue.length) {
        const id = this.queue.shift();
        if (id) await this.run(id);
      }
    } finally {
      this.pumping = false;
    }
  }
  submit(
    clientId: string,
    tool: string,
    input: JsonObject,
  ): [JsonObject, number] {
    if (!tools.has(tool))
      return [{ ok: false, error: "unknown browser tool" }, 400];
    let prompt = "";
    let topic = typeof input.topic === "string" ? input.topic : "";
    let inputJson = "{}";
    if (tool === "maps-route") {
      const start = input.start;
      const destination = input.destination;
      const mode = input.mode ?? "driving";
      if (
        typeof start !== "string" ||
        !start.trim() ||
        typeof destination !== "string" ||
        !destination.trim()
      )
        return [
          { ok: false, error: "start and destination are required" },
          400,
        ];
      if (typeof mode !== "string" || !routeModes.has(mode))
        return [
          {
            ok: false,
            error: "mode must be driving, transit, walking, or bicycling",
          },
          400,
        ];
      prompt = `Google Maps ${mode} route from ${start.trim()} to ${destination.trim()}`;
      topic ||= `${start.trim()} to ${destination.trim()}`;
      inputJson = JSON.stringify({
        start: start.trim(),
        destination: destination.trim(),
        mode,
      });
    } else {
      if (typeof input.prompt !== "string" || !input.prompt.trim())
        return [{ ok: false, error: "prompt is required" }, 400];
      prompt = input.prompt;
      inputJson = JSON.stringify({ prompt });
    }
    const timeoutValue = input.timeout_seconds ?? 300;
    const timeout = Math.max(30, Math.min(600, Number(timeoutValue)));
    if (!Number.isSafeInteger(timeout))
      return [{ ok: false, error: "timeout_seconds must be an integer" }, 400];
    const key =
      typeof input.idempotency_key === "string" ? input.idempotency_key : "";
    const fp = fingerprint(tool, inputJson, topic);
    const duplicate = this.store.activeDuplicate(clientId, fp, key);
    if (duplicate) return [publicJob(duplicate), 202];
    const id = `bt1_${randomUUID().replaceAll("-", "")}`;
    const created = timestamp();
    const directory = resolve(dataRoot, "artifacts", clientId, id);
    mkdirSync(directory, { recursive: true, mode: 0o700 });
    const filename = `${created.slice(0, 19).replaceAll("-", "").replace("T", "_").replaceAll(":", "")}_${safeSlug(topic || prompt)}.yaml`;
    const row = {
      id,
      client_id: clientId,
      tool,
      prompt,
      topic,
      input_json: inputJson,
      output_path: resolve(directory, filename),
      fingerprint: fp,
      idempotency_key: key,
      timeout_seconds: timeout,
      created_at: created,
    };
    this.store.create(row);
    this.enqueue(id);
    return [publicJob(this.store.get(id)), 202];
  }
  async run(id: string): Promise<void> {
    const row = this.store.get(id);
    if (row?.status !== "queued") return;
    this.store.update(id, "running", "acquiring_browser");
    let release: (() => void) | undefined;
    try {
      release = await this.arbiter.acquire(`job:${id}`);
      this.store.update(id, "running", "browser");
      if (row.tool === "maps-route") {
        const input = JSON.parse(row.input_json) as {
          start: string;
          destination: string;
          mode: string;
        };
        await executeBrowserRoute(
          input.start,
          input.destination,
          input.mode,
          row.output_path,
          mcpUrl,
          row.timeout_seconds,
        );
      } else {
        const platform = row.tool === "chatgpt-chat" ? "chatgpt" : "gemini";
        await executeBrowserChat(
          platform,
          row.prompt,
          row.topic,
          row.output_path,
          mcpUrl,
          row.timeout_seconds,
          dataRoot,
        );
      }
      if (!statSync(row.output_path).isFile())
        throw new Error("browser job completed without result file");
      this.store.update(id, "completed", "completed");
    } catch (error) {
      const uncertain =
        row.tool === "chatgpt-chat" || row.tool === "gemini-chat";
      this.store.update(
        id,
        uncertain ? "failed_uncertain" : "failed",
        "worker",
        error instanceof Error ? error.message : String(error),
      );
    } finally {
      release?.();
    }
  }
  status(clientId: string, id: string): [JsonObject, number] {
    const row = this.store.get(id);
    if (!row || row.client_id !== clientId)
      return [{ ok: false, error: "job not found" }, 404];
    return [publicJob(row), 200];
  }
  result(clientId: string, id: string): [Buffer, string, number] {
    const row = this.store.get(id);
    if (!row || row.client_id !== clientId)
      return [
        Buffer.from(JSON.stringify({ ok: false, error: "job not found" })),
        "application/json",
        404,
      ];
    if (row.status !== "completed")
      return [
        Buffer.from(JSON.stringify(publicJob(row))),
        "application/json",
        409,
      ];
    return [readFileSync(row.output_path), "application/yaml", 200];
  }
  cancel(clientId: string, id: string): [JsonObject, number] {
    const row = this.store.get(id);
    if (!row || row.client_id !== clientId)
      return [{ ok: false, error: "job not found" }, 404];
    if (row.status !== "queued")
      return [publicJob(row), row.status === "running" ? 409 : 200];
    this.store.update(
      id,
      "cancelled",
      "cancelled",
      "cancelled before browser submission",
    );
    return [publicJob(this.store.get(id)), 200];
  }
}

type ProxySession = {
  id: string;
  response: ServerResponse;
  client?: McpClient;
  release?: () => void;
};
const sessions = new Map<string, ProxySession>();
const jobs = new JobService();
let playwright: ChildProcess | undefined;
let stopping = false;
let playwrightRestarts = 0;

function playwrightToken(): string {
  if (!playwrightTokenFile)
    throw new Error("BROWSER_TOOLS_PLAYWRIGHT_TOKEN_FILE is required");
  const contents = readFileSync(playwrightTokenFile, "utf8");
  const assignment = contents
    .split(/\r?\n/)
    .find((line) =>
      line.trimStart().startsWith("PLAYWRIGHT_MCP_EXTENSION_TOKEN="),
    );
  const value = (
    assignment ? assignment.slice(assignment.indexOf("=") + 1) : contents
  )
    .trim()
    .replace(/^(['"])(.*)\1$/, "$2");
  if (
    !value ||
    value.includes("\n") ||
    value.includes("\r") ||
    value.includes("\0")
  )
    throw new Error("Playwright extension token file is invalid");
  return value;
}

function startPlaywright(): void {
  if (!supervisePlaywright || stopping || playwright) return;
  const token = playwrightToken();
  const executable =
    process.env.BROWSER_TOOLS_BROWSER_PATH ??
    "/Applications/Vivaldi.app/Contents/MacOS/Vivaldi";
  const profile = process.env.BROWSER_TOOLS_PROFILE ?? "Profile 1";
  const command =
    process.env.BROWSER_TOOLS_PLAYWRIGHT_COMMAND ??
    (process.platform === "darwin" ? "/opt/homebrew/bin/npx" : "npx");
  const args = [
    "--yes",
    "@playwright/mcp@latest",
    "--allowed-hosts",
    `localhost:${playwrightPort}`,
    "--host",
    "127.0.0.1",
    "--port",
    String(playwrightPort),
    "--extension",
    "--idle-timeout",
    "0",
    "--shared-browser-context",
    "--profile-dir-name",
    profile,
    "--output-dir",
    resolve(dataRoot, "playwright-output"),
  ];
  playwright = spawn(command, args, {
    cwd: dataRoot,
    env: {
      ...process.env,
      PLAYWRIGHT_MCP_EXTENSION_TOKEN: token,
      PLAYWRIGHT_MCP_EXECUTABLE_PATH: executable,
    },
    stdio: "inherit",
  });
  const processChild = playwright;
  const child = processChild as unknown as {
    on(event: "error", listener: (error: Error) => void): void;
    on(
      event: "exit",
      listener: (code: number | null, signal: string | null) => void,
    ): void;
  };
  child.on("error", (error) =>
    console.error(`playwright-mcp error: ${error.message}`),
  );
  child.on("exit", (code, signal) => {
    if (playwright === processChild) playwright = undefined;
    if (!stopping) {
      playwrightRestarts += 1;
      setTimeout(
        startPlaywright,
        Math.min(30_000, 1_000 * 2 ** Math.min(playwrightRestarts, 5)),
      );
    }
    console.error(
      `playwright-mcp exited code=${code ?? "null"} signal=${signal ?? "null"}`,
    );
  });
}

function sse(response: ServerResponse, event: string, value: unknown): void {
  response.write(`event: ${event}\ndata: ${JSON.stringify(value)}\n\n`);
}

async function ensureMcp(session: ProxySession): Promise<McpClient> {
  if (!session.client) {
    session.client = new McpClient(mcpUrl);
    await session.client.connect();
  }
  return session.client;
}

async function proxyMessage(
  session: ProxySession,
  message: JsonObject,
): Promise<void> {
  const id = message.id;
  const method = typeof message.method === "string" ? message.method : "";
  if (method === "initialize") {
    if (id !== undefined)
      sse(session.response, "message", {
        jsonrpc: "2.0",
        id,
        result: {
          protocolVersion: "2024-11-05",
          capabilities: { tools: {} },
          serverInfo: { name: "openlia-browser-tools", version: "0.1.0" },
        },
      });
    return;
  }
  if (method === "notifications/initialized") return;
  const client = await ensureMcp(session);
  if (method === "tools/call") {
    if (!session.release)
      session.release = await jobs.arbiter.acquire(
        `interactive:${session.id}`,
        30_000,
      );
    const params = (message.params ?? {}) as JsonObject;
    const result = await client.callTool(
      String(params.name ?? ""),
      (params.arguments ?? {}) as Record<string, unknown>,
    );
    if (id !== undefined)
      sse(session.response, "message", { jsonrpc: "2.0", id, result });
    return;
  }
  const result = await client.call(
    method,
    (message.params ?? {}) as Record<string, unknown>,
  );
  if (id !== undefined)
    sse(session.response, "message", { jsonrpc: "2.0", id, result });
}

async function readMessages(request: IncomingMessage): Promise<JsonObject[]> {
  const parsed = await body(request);
  return parsed ? [parsed] : [];
}

async function handler(
  request: IncomingMessage,
  response: ServerResponse,
): Promise<void> {
  if (!authorized(request)) {
    json(response, { ok: false, error: "unauthorized" }, 401);
    return;
  }
  const url = new URL(
    request.url ?? "/",
    `http://${request.headers.host ?? "localhost"}`,
  );
  if (request.method === "GET" && url.pathname === "/health") {
    json(response, {
      ok: true,
      service: "browser-tools",
      mcp_url: mcpUrl,
      playwright_pid: playwright?.pid ?? null,
      browser_lease: jobs.arbiter.status(),
    });
    return;
  }
  if (request.method === "GET" && url.pathname === "/v1/info") {
    json(response, {
      ok: true,
      service: "browser-tools",
      protocol_version: 1,
      mcp_url: mcpUrl,
    });
    return;
  }
  const clientId = String(request.headers["x-openlia-client-id"] ?? "default");
  if (request.method === "POST" && url.pathname === "/v1/jobs") {
    const input = await body(request);
    if (!input)
      return json(
        response,
        { ok: false, error: "body must be a JSON object under 1 MiB" },
        400,
      );
    const [result, status] = jobs.submit(
      clientId,
      String(input.tool ?? ""),
      input,
    );
    json(response, result, status);
    return;
  }
  const parts = url.pathname.split("/").filter(Boolean);
  if (
    request.method === "POST" &&
    parts[0] === "openlia" &&
    parts[1] === "tools" &&
    parts[2]
  ) {
    const input = await body(request);
    if (!input)
      return json(
        response,
        { ok: false, error: "body must be a JSON object under 1 MiB" },
        400,
      );
    const [result, status] = jobs.submit(clientId, parts[2], input);
    json(response, result, status);
    return;
  }
  if (parts[0] === "openlia" && parts[1] === "jobs" && parts[2]) {
    const id = parts[2];
    if (request.method === "GET" && parts[3] === "result") {
      const [result, type, status] = jobs.result(clientId, id);
      bytes(response, result, type, status);
      return;
    }
    if (request.method === "POST" && parts[3] === "cancel") {
      const [result, status] = jobs.cancel(clientId, id);
      json(response, result, status);
      return;
    }
    if (request.method === "GET") {
      const [result, status] = jobs.status(clientId, id);
      json(response, result, status);
      return;
    }
  }
  if (parts[0] === "v1" && parts[1] === "jobs" && parts[2]) {
    const id = parts[2];
    if (request.method === "GET" && parts[3] === "result") {
      const [result, type, status] = jobs.result(clientId, id);
      bytes(response, result, type, status);
      return;
    }
    if (request.method === "POST" && parts[3] === "cancel") {
      const [result, status] = jobs.cancel(clientId, id);
      json(response, result, status);
      return;
    }
    if (request.method === "GET") {
      const [result, status] = jobs.status(clientId, id);
      json(response, result, status);
      return;
    }
  }
  if (request.method === "GET" && url.pathname === "/sse") {
    const id = randomUUID();
    const session: ProxySession = { id, response };
    sessions.set(id, session);
    response.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
    });
    response.write(
      `event: endpoint\ndata: /messages?sessionId=${encodeURIComponent(id)}\n\n`,
    );
    request.on("close", () => {
      sessions.delete(id);
      session.release?.();
      session.client?.close();
    });
    return;
  }
  if (request.method === "POST" && url.pathname === "/messages") {
    const id = url.searchParams.get("sessionId") ?? "";
    const session = sessions.get(id);
    if (!session)
      return json(response, { ok: false, error: "MCP session not found" }, 404);
    for (const message of await readMessages(request)) {
      try {
        await proxyMessage(session, message);
      } catch (error) {
        if (message.id !== undefined)
          sse(session.response, "message", {
            jsonrpc: "2.0",
            id: message.id,
            error: {
              code: -32000,
              message: error instanceof Error ? error.message : String(error),
            },
          });
      }
    }
    response.writeHead(202).end();
    return;
  }
  json(response, { ok: false, error: "not found" }, 404);
}

const server = createServer((request, response) => {
  void handler(request, response).catch((error) => {
    if (!response.headersSent)
      json(response, { ok: false, error: String(error) }, 500);
    else response.destroy();
  });
});

server.listen(port, bind, () => {
  console.log(`browser-tools listening on http://${bind}:${port}`);
  try {
    startPlaywright();
  } catch (error) {
    console.error(
      `playwright-mcp startup failed: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
});

function shutdown(): void {
  stopping = true;
  for (const session of sessions.values()) {
    session.release?.();
    session.client?.close();
    session.response.end();
  }
  jobs.store.db.close();
  playwright?.kill("SIGTERM");
  playwright = undefined;
  server.close();
}

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
