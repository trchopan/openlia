import { Database } from "bun:sqlite";
import { createHash } from "node:crypto";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  readFileSync,
  statSync,
} from "node:fs";
import { dirname, relative, resolve, sep } from "node:path";
import { executeBrowserChat } from "./browser";

const maxBodyBytes = 1024 * 1024;
const dataRoot = resolve(process.env.OPENLIA_TOOLS_DATA_ROOT ?? "/opt/data");
const workspaceRoot = resolve(dataRoot, "workspace");
const databasePath = resolve(dataRoot, "openlia", "jobs.sqlite3");
const tools = new Set(["chatgpt-chat", "gemini-chat"]);

type JobStatus = "queued" | "running" | "completed" | "failed" | "cancelled";
type JobRow = {
  id: string;
  tool: string;
  prompt: string;
  topic: string;
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
type JsonObject = Record<string, unknown>;

function timestamp(): string {
  const value = new Date().toISOString();
  return `${value.slice(0, -1)}000+00:00`;
}

function redact(value: string): string {
  return value
    .replace(/bearer\s+[A-Za-z0-9._~+/=-]+/gi, "Bearer [REDACTED]")
    .replace(/\b(?:sk|ghp|gho|ghu|github_pat)_[A-Za-z0-9_-]+/g, "[REDACTED]")
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
      .slice(0, 50) || "chat"
  );
}

function fingerprint(tool: string, prompt: string, topic: string): string {
  return createHash("sha256")
    .update(`${tool}\0${prompt}\0${topic}`)
    .digest("hex");
}

function publicJob(row: JobRow | null): JsonObject {
  if (!row) return { ok: false, error: "job not found" };
  const relativePath = row.output_path.startsWith(workspaceRoot + sep)
    ? relative(workspaceRoot, row.output_path).split(sep).join("/")
    : null;
  const result: JsonObject = {
    ok: true,
    job_id: row.id,
    tool: row.tool,
    status: row.status,
    phase: row.phase,
    created_at: row.created_at,
    updated_at: row.updated_at,
    result_path: relativePath,
  };
  if (row.error) result.error = row.error;
  return result;
}

class JobStore {
  readonly db: Database;
  constructor() {
    mkdirSync(dirname(databasePath), { recursive: true, mode: 0o700 });
    this.db = new Database(databasePath, { create: true, readwrite: true });
    this.db.exec("PRAGMA journal_mode=WAL");
    this.db.exec(`CREATE TABLE IF NOT EXISTS jobs (
      id TEXT PRIMARY KEY, tool TEXT NOT NULL, prompt TEXT NOT NULL, topic TEXT NOT NULL,
      output_path TEXT NOT NULL, fingerprint TEXT NOT NULL, idempotency_key TEXT NOT NULL,
      timeout_seconds INTEGER NOT NULL, status TEXT NOT NULL, phase TEXT NOT NULL,
      error TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
    )`);
    try {
      chmodSync(databasePath, 0o600);
    } catch {
      /* mode is best effort on some mounts */
    }
  }
  recoverRunning(): void {
    this.db
      .query(
        "UPDATE jobs SET status='failed', phase='worker_restart', error=?, updated_at=? WHERE status='running'",
      )
      .run(
        "worker restarted while job was running; prompt was not retried",
        timestamp(),
      );
  }
  pruneOlderThan(days = 14): number {
    const cutoff = new Date(Date.now() - days * 86_400_000)
      .toISOString()
      .replace("Z", "000+00:00");
    const result = this.db
      .query(
        "DELETE FROM jobs WHERE status IN ('completed','failed','cancelled') AND created_at < ?",
      )
      .run(cutoff);
    return Number(result.changes ?? 0);
  }
  activeDuplicate(
    fingerprintValue: string,
    idempotencyKey: string,
  ): JobRow | null {
    const query = idempotencyKey
      ? this.db.query(
          "SELECT * FROM jobs WHERE idempotency_key=? AND status IN ('queued','running') ORDER BY created_at DESC LIMIT 1",
        )
      : this.db.query(
          "SELECT * FROM jobs WHERE fingerprint=? AND status IN ('queued','running') ORDER BY created_at DESC LIMIT 1",
        );
    return (
      (query.get(idempotencyKey || fingerprintValue) as JobRow | null) ?? null
    );
  }
  create(values: {
    id: string;
    tool: string;
    prompt: string;
    topic: string;
    outputPath: string;
    fingerprint: string;
    idempotencyKey: string;
    timeout: number;
    created: string;
  }): void {
    this.db
      .query(
        `INSERT INTO jobs (id,tool,prompt,topic,output_path,fingerprint,idempotency_key,timeout_seconds,status,phase,error,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,'queued','queued','',?,?)`,
      )
      .run(
        values.id,
        values.tool,
        values.prompt,
        values.topic,
        values.outputPath,
        values.fingerprint,
        values.idempotencyKey,
        values.timeout,
        values.created,
        values.created,
      );
  }
  get(id: string): JobRow | null {
    return (
      (this.db
        .query("SELECT * FROM jobs WHERE id=?")
        .get(id) as JobRow | null) ?? null
    );
  }
  queued(): JobRow[] {
    return this.db
      .query("SELECT * FROM jobs WHERE status='queued' ORDER BY created_at")
      .all() as JobRow[];
  }
  update(id: string, status: JobStatus, phase: string, error = ""): void {
    this.db
      .query("UPDATE jobs SET status=?,phase=?,error=?,updated_at=? WHERE id=?")
      .run(status, phase, redact(error).slice(0, 2000), timestamp(), id);
  }
}

class JobService {
  readonly store = new JobStore();
  readonly queue: string[] = [];
  activeJobId: string | null = null;
  pumping = false;
  constructor() {
    this.store.recoverRunning();
    this.store.pruneOlderThan();
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
  submit(tool: string, body: JsonObject): [JsonObject, number] {
    if (!tools.has(tool))
      return [{ ok: false, error: "unknown browser tool" }, 400];
    const prompt = body.prompt;
    if (
      typeof prompt !== "string" ||
      !prompt.trim() ||
      [...prompt].length > 200_000
    )
      return [
        {
          ok: false,
          error: "prompt must be a non-empty string under 200000 characters",
        },
        400,
      ];
    const topic = body.topic ?? "";
    if (typeof topic !== "string")
      return [{ ok: false, error: "topic must be a string" }, 400];
    const rawTimeout = body.timeout_seconds ?? 300;
    const validTimeoutNumber =
      typeof rawTimeout === "number" &&
      Number.isSafeInteger(rawTimeout) &&
      rawTimeout >= 0;
    const validTimeoutString =
      typeof rawTimeout === "string" && /^\d+$/.test(rawTimeout.trim());
    if (!validTimeoutNumber && !validTimeoutString) {
      return [{ ok: false, error: "timeout_seconds must be an integer" }, 400];
    }
    let timeout = Number(rawTimeout);
    timeout = Math.max(30, Math.min(timeout, 600));
    const key = body.idempotency_key ?? "";
    if (typeof key !== "string")
      return [{ ok: false, error: "idempotency_key must be a string" }, 400];
    const fp = fingerprint(tool, prompt, topic);
    const duplicate = this.store.activeDuplicate(fp, key);
    if (duplicate) return [publicJob(duplicate), 202];
    const id = `job-${crypto.randomUUID().replaceAll("-", "")}`;
    const platform = tool === "chatgpt-chat" ? "chatgpt" : "gemini";
    const created = timestamp();
    const filename = `${created.slice(0, 19).replaceAll("-", "").replace("T", "_").replaceAll(":", "")}_${safeSlug(topic || prompt.slice(0, 40))}_${id.slice(4, 12)}.yaml`;
    const outputPath = resolve(workspaceRoot, "knowledge", platform, filename);
    this.store.create({
      id,
      tool,
      prompt,
      topic,
      outputPath,
      fingerprint: fp,
      idempotencyKey: key,
      timeout,
      created,
    });
    this.enqueue(id);
    return [publicJob(this.store.get(id)), 202];
  }
  async run(id: string): Promise<void> {
    const row = this.store.get(id);
    if (!row || row.status !== "queued") return;
    this.store.update(id, "running", "running");
    this.activeJobId = id;
    try {
      const platform = row.tool === "chatgpt-chat" ? "chatgpt" : "gemini";
      await executeBrowserChat(
        platform,
        row.prompt,
        row.topic,
        row.output_path,
        process.env.OPENLIA_BROWSER_MCP_URL ?? "",
        row.timeout_seconds,
        dataRoot,
      );
      if (!existsSync(row.output_path) || !statSync(row.output_path).isFile())
        this.store.update(
          id,
          "failed",
          "worker",
          "browser job completed without a result file",
        );
      else this.store.update(id, "completed", "completed");
    } catch (error) {
      this.store.update(
        id,
        "failed",
        "worker",
        error instanceof Error ? (error.stack ?? error.message) : String(error),
      );
    } finally {
      this.activeJobId = null;
    }
  }
  health(): JsonObject {
    return {
      ok: true,
      service: "openlia-tools",
      active_jobs: this.activeJobId ? 1 : 0,
      browser_mcp_url: process.env.OPENLIA_BROWSER_MCP_URL ?? "",
    };
  }
  status(id: string): [JsonObject, number] {
    const row = this.store.get(id);
    return row
      ? [publicJob(row), 200]
      : [{ ok: false, error: "job not found" }, 404];
  }
  result(id: string): [Uint8Array, string, number] {
    const row = this.store.get(id);
    if (!row)
      return [
        new TextEncoder().encode('{"ok":false,"error":"job not found"}'),
        "application/json",
        404,
      ];
    if (row.status !== "completed")
      return [
        new TextEncoder().encode(JSON.stringify(publicJob(row))),
        "application/json",
        409,
      ];
    try {
      return [readFileSync(row.output_path), "application/yaml", 200];
    } catch {
      return [
        new TextEncoder().encode(
          '{"ok":false,"error":"job result is missing"}',
        ),
        "application/json",
        500,
      ];
    }
  }
  cancel(id: string): [JsonObject, number] {
    const row = this.store.get(id);
    if (!row) return [{ ok: false, error: "job not found" }, 404];
    if (row.status === "queued") {
      this.store.update(
        id,
        "cancelled",
        "cancelled",
        "cancelled before browser submission",
      );
      return [publicJob(this.store.get(id)), 200];
    }
    if (row.status === "running")
      return [
        {
          ok: false,
          error:
            "running browser jobs cannot be cancelled safely after prompt submission",
        },
        409,
      ];
    return [publicJob(row), 200];
  }
}

function responseJson(payload: JsonObject, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
    },
  });
}
function responseBytes(
  data: Uint8Array,
  type: string,
  status: number,
): Response {
  return new Response(data as unknown as BodyInit, {
    status,
    headers: {
      "Content-Type": type,
      "Content-Length": String(data.byteLength),
    },
  });
}
function normalizedPath(request: Request): string {
  return new URL(request.url).pathname.replace(/\/+$/, "");
}

const service = new JobService();
async function requestBody(request: Request): Promise<JsonObject | null> {
  const lengthHeader = request.headers.get("content-length");
  if (lengthHeader !== null) {
    const length = Number(lengthHeader);
    if (!Number.isSafeInteger(length) || length <= 0 || length > maxBodyBytes)
      return null;
  }
  if (!request.body) return null;
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    while (true) {
      const next = await reader.read();
      if (next.done) break;
      total += next.value.byteLength;
      if (total > maxBodyBytes) {
        await reader.cancel();
        return null;
      }
      chunks.push(next.value);
    }
  } finally {
    reader.releaseLock();
  }
  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  try {
    const value: unknown = JSON.parse(new TextDecoder().decode(bytes));
    return value && typeof value === "object" && !Array.isArray(value)
      ? (value as JsonObject)
      : null;
  } catch {
    return null;
  }
}

async function fetchHandler(request: Request): Promise<Response> {
  const path = normalizedPath(request);
  if (request.method === "GET" && path === "/health")
    return responseJson(service.health());
  if (request.method === "GET" && path.startsWith("/openlia/jobs/")) {
    const parts = path.split("/");
    if (parts.length === 4) {
      const [body, status] = service.status(parts[3] ?? "");
      return responseJson(body, status);
    }
    if (parts.length === 5 && parts[4] === "result") {
      const [data, type, status] = service.result(parts[3] ?? "");
      return responseBytes(data, type, status);
    }
  }
  if (request.method === "POST") {
    const body = await requestBody(request);
    if (!body)
      return responseJson(
        { ok: false, error: "request body must be a JSON object under 1 MiB" },
        400,
      );
    if (path.startsWith("/openlia/tools/")) {
      const [result, status] = service.submit(
        path.split("/").at(-1) ?? "",
        body,
      );
      return responseJson(result, status);
    }
    if (path.startsWith("/openlia/jobs/") && path.endsWith("/cancel")) {
      const [result, status] = service.cancel(path.split("/")[3] ?? "");
      return responseJson(result, status);
    }
  }
  return responseJson({ ok: false, error: "not found" }, 404);
}

const bind = process.env.OPENLIA_TOOLS_BIND ?? "0.0.0.0";
const port = Number(process.env.OPENLIA_TOOLS_PORT ?? "8787");
const server = Bun.serve({ hostname: bind, port, fetch: fetchHandler });
const shutdown = (): void => {
  server.stop(true);
  service.store.db.close();
};
process.on("SIGTERM", shutdown);
process.on("SIGINT", shutdown);
console.log(`openlia-tools listening on ${bind}:${port}`);
