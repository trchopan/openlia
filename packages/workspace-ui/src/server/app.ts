import { existsSync, lstatSync } from "node:fs";
import { join, resolve, sep } from "node:path";
import type {
  AuthLoginResponse,
  SkillCreateRequest,
  SkillToggleEnableRequest,
  SkillTogglePinRequest,
  SkillWriteRequest,
  WorkspaceErrorResponse,
  WorkspaceWriteRequest,
} from "../shared/api";
import { Authenticator } from "./auth";
import { SkillService } from "./skills";
import { WorkspaceError, WorkspaceService } from "./workspace";

export interface WorkspaceHandlerOptions {
  workspaceRoot: string;
  skillsRoot?: string;
  staticRoot?: string;
  maxEditableBytes?: number;
  maxDownloadBytes?: number;
  maxTreeEntries?: number;
  authRequired?: boolean;
  passwordHash?: string | undefined;
  passwordHashFile?: string | undefined;
  publicOrigin?: string | undefined;
  secureCookies?: boolean | undefined;
}

const contentTypes: Record<string, string> = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".map": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
};
const maxLoginRequestBytes = 16 * 1024;

function securityHeaders(): Record<string, string> {
  return {
    "Cache-Control": "no-store",
    "Content-Security-Policy":
      "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'none'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'",
    "Referrer-Policy": "no-referrer",
    "X-Content-Type-Options": "nosniff",
  };
}

function json(
  payload: object,
  status = 200,
  extraHeaders: Record<string, string> = {},
): Response {
  return new Response(JSON.stringify(payload), {
    headers: {
      ...securityHeaders(),
      "Content-Type": "application/json; charset=utf-8",
      ...extraHeaders,
    },
    status,
  });
}

function errorResponse(error: unknown): Response {
  if (error instanceof WorkspaceError) {
    const payload: WorkspaceErrorResponse = {
      schema: 1,
      ok: false,
      error: error.code,
    };
    if (error.currentRevision !== undefined)
      payload.current_revision = error.currentRevision;
    return json(payload, error.status);
  }
  return json({ schema: 1, ok: false, error: "request_failed" }, 500);
}

function staticFile(
  staticRoot: string,
  pathname: string,
): Response | undefined {
  const relativePath = pathname === "/" ? "index.html" : pathname.slice(1);
  if (relativePath !== "index.html" && !relativePath.startsWith("assets/"))
    return undefined;
  let decodedPath: string;
  try {
    decodedPath = decodeURIComponent(relativePath);
  } catch {
    return undefined;
  }
  const root = resolve(staticRoot);
  const absolute = resolve(root, decodedPath);
  if (absolute !== root && !absolute.startsWith(root + sep)) return undefined;
  if (!existsSync(absolute) || !lstatSync(absolute).isFile()) return undefined;
  const extension = absolute.slice(absolute.lastIndexOf(".")).toLowerCase();
  const headers = {
    ...securityHeaders(),
    "Cache-Control":
      relativePath === "index.html"
        ? "no-store"
        : "public, max-age=31536000, immutable",
    "Content-Type": contentTypes[extension] ?? "application/octet-stream",
  };
  return new Response(Bun.file(absolute), { headers });
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function readJson(
  request: Request,
  maxBytes: number,
): Promise<{ value: unknown; tooLarge: boolean }> {
  const lengthHeader = request.headers.get("content-length");
  if (lengthHeader !== null) {
    const length = Number(lengthHeader);
    if (!Number.isSafeInteger(length) || length < 0 || length > maxBytes)
      return { value: null, tooLarge: true };
  }
  if (!request.body) return { value: null, tooLarge: false };
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    while (true) {
      const next = await reader.read();
      if (next.done) break;
      total += next.value.byteLength;
      if (total > maxBytes) {
        await reader.cancel();
        return { value: null, tooLarge: true };
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
    return {
      value: JSON.parse(new TextDecoder().decode(bytes)),
      tooLarge: false,
    };
  } catch {
    return { value: null, tooLarge: false };
  }
}

export function createWorkspaceHandler(
  options: WorkspaceHandlerOptions,
): (request: Request, clientKey?: string) => Promise<Response> {
  const service = new WorkspaceService(options);
  const skillsRoot =
    options.skillsRoot ??
    process.env.OPENLIA_SKILLS_ROOT ??
    join(options.workspaceRoot, "..", "skills");
  const skillService = new SkillService({
    maxEditableBytes: options.maxEditableBytes,
    skillsRoot,
  });
  const staticRoot = options.staticRoot ?? join(import.meta.dir, "public");
  const publicOrigin = normalizeOrigin(options.publicOrigin);
  if (options.publicOrigin && !publicOrigin)
    throw new Error("workspace-ui public origin is invalid");
  const authenticator = new Authenticator({
    passwordHash: options.passwordHash,
    passwordHashFile: options.passwordHashFile,
    required: options.authRequired ?? false,
    secureCookies: options.secureCookies,
  });

  return async (request: Request, clientKey = "global"): Promise<Response> => {
    try {
      const url = new URL(request.url);
      if (request.method === "GET" && url.pathname === "/health") {
        return json({ schema: 1, ok: true, service: "workspace-ui" });
      }
      if (request.method === "GET" && url.pathname.startsWith("/assets/")) {
        return (
          staticFile(staticRoot, url.pathname) ??
          json({ schema: 1, ok: false, error: "not_found" }, 404)
        );
      }
      if (
        request.method === "GET" &&
        (url.pathname === "/" ||
          url.pathname === "/index.html" ||
          url.pathname.startsWith("/files/") ||
          url.pathname === "/files" ||
          url.pathname.startsWith("/file/") ||
          url.pathname === "/file" ||
          url.pathname.startsWith("/skills/") ||
          url.pathname === "/skills")
      ) {
        return (
          staticFile(staticRoot, "/") ??
          json({ schema: 1, ok: false, error: "not_found" }, 404)
        );
      }
      if (request.method === "GET" && url.pathname === "/api/auth/session") {
        return json(authenticator.sessionResponse(request));
      }
      if (request.method === "POST" && url.pathname === "/api/auth/login") {
        if (!sameOrigin(request, url, publicOrigin))
          return json(
            { schema: 1, ok: false, error: "origin_not_allowed" },
            403,
          );
        const bodyResult = await readJson(request, maxLoginRequestBytes);
        if (bodyResult.tooLarge)
          return json(
            { schema: 1, ok: false, error: "request_too_large" },
            413,
          );
        const body: unknown = bodyResult.value;
        if (!isObject(body) || typeof body.password !== "string")
          return json({ schema: 1, ok: false, error: "invalid_login" }, 400);
        const result = await authenticator.login(
          body.password,
          clientKey,
          secureRequest(request, url, publicOrigin),
        );
        if (result.kind === "rate_limited")
          return json({ schema: 1, ok: false, error: "rate_limited" }, 429);
        if (result.kind === "invalid")
          return json({ schema: 1, ok: false, error: "invalid_login" }, 401);
        const payload: AuthLoginResponse = { schema: 1, ok: true };
        return json(
          payload,
          200,
          result.cookie ? { "Set-Cookie": result.cookie } : {},
        );
      }
      if (request.method === "POST" && url.pathname === "/api/auth/logout") {
        if (!sameOrigin(request, url, publicOrigin))
          return json(
            { schema: 1, ok: false, error: "origin_not_allowed" },
            403,
          );
        return json({ schema: 1, ok: true }, 200, {
          "Set-Cookie": authenticator.logout(
            request,
            secureRequest(request, url, publicOrigin),
          ),
        });
      }
      if (
        (url.pathname.startsWith("/api/workspace/") ||
          url.pathname.startsWith("/api/skills/")) &&
        !authenticator.isAuthenticated(request)
      ) {
        return json(
          { schema: 1, ok: false, error: "authentication_required" },
          401,
        );
      }
      if (request.method === "GET" && url.pathname === "/api/workspace/tree") {
        return json(service.tree());
      }
      if (request.method === "GET" && url.pathname === "/api/workspace/file") {
        return json(service.read(url.searchParams.get("path")));
      }
      if (
        request.method === "GET" &&
        url.pathname === "/api/workspace/download"
      ) {
        const file = service.download(url.searchParams.get("path"));
        return new Response(Bun.file(file.absolute), {
          headers: {
            ...securityHeaders(),
            "Content-Disposition": `attachment; filename="${file.filename}"`,
            "Content-Type": "application/octet-stream",
          },
        });
      }
      if (
        request.method === "GET" &&
        url.pathname === "/api/workspace/git/status"
      ) {
        return json(await service.gitStatus());
      }
      if (request.method === "PUT" && url.pathname === "/api/workspace/file") {
        if (!sameOrigin(request, url, publicOrigin))
          return json(
            { schema: 1, ok: false, error: "origin_not_allowed" },
            403,
          );
        const bodyResult = await readJson(
          request,
          (options.maxEditableBytes ?? 2 * 1024 * 1024) + 4096,
        );
        if (bodyResult.tooLarge)
          return json(
            { schema: 1, ok: false, error: "request_too_large" },
            413,
          );
        const body: unknown = bodyResult.value;
        if (!isObject(body))
          return json(
            { schema: 1, ok: false, error: "request_body_must_be_json" },
            400,
          );
        const writeRequest = body as Partial<WorkspaceWriteRequest>;
        return json(
          service.write(
            writeRequest.path,
            writeRequest.content,
            writeRequest.expected_revision,
          ),
        );
      }
      if (request.method === "GET" && url.pathname === "/api/skills/list") {
        return json(skillService.list());
      }
      if (request.method === "GET" && url.pathname === "/api/skills/detail") {
        return json(skillService.detail(url.searchParams.get("id")));
      }
      if (request.method === "GET" && url.pathname === "/api/skills/file") {
        return json(
          skillService.readFile(
            url.searchParams.get("id"),
            url.searchParams.get("path"),
          ),
        );
      }
      if (request.method === "PUT" && url.pathname === "/api/skills/file") {
        if (!sameOrigin(request, url, publicOrigin))
          return json(
            { schema: 1, ok: false, error: "origin_not_allowed" },
            403,
          );
        const bodyResult = await readJson(
          request,
          (options.maxEditableBytes ?? 2 * 1024 * 1024) + 4096,
        );
        if (bodyResult.tooLarge)
          return json(
            { schema: 1, ok: false, error: "request_too_large" },
            413,
          );
        const body: unknown = bodyResult.value;
        if (!isObject(body))
          return json(
            { schema: 1, ok: false, error: "request_body_must_be_json" },
            400,
          );
        const writeRequest = body as Partial<SkillWriteRequest>;
        return json(
          skillService.writeFile(
            writeRequest.id,
            writeRequest.path,
            writeRequest.content,
            writeRequest.expected_revision,
          ),
        );
      }
      if (
        request.method === "POST" &&
        url.pathname === "/api/skills/toggle-enable"
      ) {
        if (!sameOrigin(request, url, publicOrigin))
          return json(
            { schema: 1, ok: false, error: "origin_not_allowed" },
            403,
          );
        const bodyResult = await readJson(request, 4096);
        const body: unknown = bodyResult.value;
        if (!isObject(body) || typeof body.enabled !== "boolean")
          return json({ schema: 1, ok: false, error: "invalid_request" }, 400);
        const req = body as Partial<SkillToggleEnableRequest>;
        skillService.toggleEnable(req.id, Boolean(body.enabled));
        return json({ schema: 1, ok: true });
      }
      if (
        request.method === "POST" &&
        url.pathname === "/api/skills/toggle-pin"
      ) {
        if (!sameOrigin(request, url, publicOrigin))
          return json(
            { schema: 1, ok: false, error: "origin_not_allowed" },
            403,
          );
        const bodyResult = await readJson(request, 4096);
        const body: unknown = bodyResult.value;
        if (!isObject(body) || typeof body.pinned !== "boolean")
          return json({ schema: 1, ok: false, error: "invalid_request" }, 400);
        const req = body as Partial<SkillTogglePinRequest>;
        skillService.togglePin(req.id, Boolean(body.pinned));
        return json({ schema: 1, ok: true });
      }
      if (request.method === "POST" && url.pathname === "/api/skills/create") {
        if (!sameOrigin(request, url, publicOrigin))
          return json(
            { schema: 1, ok: false, error: "origin_not_allowed" },
            403,
          );
        const bodyResult = await readJson(request, 4096);
        const body: unknown = bodyResult.value;
        if (!isObject(body))
          return json({ schema: 1, ok: false, error: "invalid_request" }, 400);
        const req = body as Partial<SkillCreateRequest>;
        const createdId = skillService.create(
          req.name,
          req.category,
          req.description,
        );
        return json({ id: createdId, ok: true, schema: 1 }, 201);
      }
      return json({ schema: 1, ok: false, error: "not_found" }, 404);
    } catch (error) {
      return errorResponse(error);
    }
  };
}

function sameOrigin(
  request: Request,
  url: URL,
  publicOrigin: string | undefined,
): boolean {
  const origin = request.headers.get("origin");
  if (!origin || origin === url.origin) return true;
  if (publicOrigin && origin === publicOrigin) return true;
  return origin === forwardedOrigin(request, url);
}

function secureRequest(
  request: Request,
  url: URL,
  publicOrigin: string | undefined,
): boolean {
  if (url.protocol === "https:") return true;
  if (publicOrigin && request.headers.get("origin") === publicOrigin)
    return true;
  return forwardedOrigin(request, url)?.startsWith("https://") ?? false;
}

function forwardedOrigin(request: Request, url: URL): string | undefined {
  const forwarded = parseForwarded(request.headers.get("forwarded"));
  const protocol =
    firstHeaderValue(request.headers.get("x-forwarded-proto")) ??
    forwarded.proto ??
    url.protocol.slice(0, -1);
  const host =
    firstHeaderValue(request.headers.get("x-forwarded-host")) ??
    forwarded.host ??
    url.host;
  if (protocol !== "http" && protocol !== "https") return undefined;
  try {
    const candidate = new URL(`${protocol}://${host}`);
    if (
      candidate.username ||
      candidate.password ||
      candidate.pathname !== "/" ||
      candidate.search ||
      candidate.hash
    )
      return undefined;
    return candidate.origin;
  } catch {
    return undefined;
  }
}

function firstHeaderValue(value: string | null): string | undefined {
  const first = value?.split(",", 1)[0]?.trim();
  return first || undefined;
}

function parseForwarded(value: string | null): {
  host?: string;
  proto?: string;
} {
  const first = firstHeaderValue(value);
  if (!first) return {};
  const result: { host?: string; proto?: string } = {};
  for (const item of first.split(";")) {
    const separator = item.indexOf("=");
    if (separator < 0) continue;
    const key = item.slice(0, separator).trim().toLowerCase();
    const parameter = item
      .slice(separator + 1)
      .trim()
      .replace(/^"(.*)"$/, "$1");
    if (key === "host") result.host = parameter;
    if (key === "proto") result.proto = parameter;
  }
  return result;
}

function normalizeOrigin(value: string | undefined): string | undefined {
  if (!value) return undefined;
  try {
    const origin = new URL(value);
    if (
      (origin.protocol !== "http:" && origin.protocol !== "https:") ||
      origin.username ||
      origin.password ||
      origin.pathname !== "/" ||
      origin.search ||
      origin.hash
    )
      return undefined;
    return origin.origin;
  } catch {
    return undefined;
  }
}
