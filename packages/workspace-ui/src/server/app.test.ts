import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createWorkspaceHandler } from "./app";
import { revision } from "./workspace";

let root = "";
let handler: ReturnType<typeof createWorkspaceHandler>;

function request(path: string, init?: RequestInit): Promise<Response> {
  return handler(new Request(`http://localhost${path}`, init));
}

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "openlia-workspace-ui-"));
  handler = createWorkspaceHandler({ workspaceRoot: root });
});

afterEach(() => {
  rmSync(root, { force: true, recursive: true });
});

describe("workspace HTTP handler", () => {
  test("serves health and security headers", async () => {
    const response = await request("/health");

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({
      schema: 1,
      ok: true,
      service: "workspace-ui",
    });
    expect(response.headers.get("x-content-type-options")).toBe("nosniff");
    expect(response.headers.get("content-security-policy")).toContain(
      "script-src 'self'",
    );
  });

  test("lists files while excluding protected paths and symlinks", async () => {
    mkdirSync(join(root, "notes"));
    writeFileSync(join(root, "notes", "readme.md"), "hello");
    writeFileSync(join(root, ".env"), "secret");
    symlinkSync(join(root, "notes", "readme.md"), join(root, "linked.md"));

    const response = await request("/api/workspace/tree");
    const payload = (await response.json()) as {
      entries: Array<{ path: string; kind: string }>;
    };

    expect(payload.entries).toEqual([
      { kind: "directory", path: "notes" },
      expect.objectContaining({ kind: "file", path: "notes/readme.md" }),
    ]);
  });

  test("reads, writes, and rejects stale revisions", async () => {
    writeFileSync(join(root, "note.md"), "before");
    const readResponse = await request("/api/workspace/file?path=note.md");
    const document = (await readResponse.json()) as {
      revision: string;
      content: string;
    };
    expect(document.content).toBe("before");

    const content = JSON.stringify({
      content: "after",
      expected_revision: document.revision,
      path: "note.md",
    });
    const writeResponse = await request("/api/workspace/file", {
      body: content,
      headers: {
        "Content-Length": String(Buffer.byteLength(content)),
        "Content-Type": "application/json",
      },
      method: "PUT",
    });
    expect(writeResponse.status).toBe(200);
    expect(readFileSync(join(root, "note.md"), "utf8")).toBe("after");

    const stale = JSON.stringify({
      content: "lost",
      expected_revision: document.revision,
      path: "note.md",
    });
    const conflictResponse = await request("/api/workspace/file", {
      body: stale,
      headers: {
        "Content-Length": String(Buffer.byteLength(stale)),
        "Content-Type": "application/json",
      },
      method: "PUT",
    });
    expect(conflictResponse.status).toBe(409);
    expect(await conflictResponse.json()).toEqual({
      current_revision: revision(new TextEncoder().encode("after")),
      error: "revision_conflict",
      ok: false,
      schema: 1,
    });
  });

  test("rejects protected paths and cross-origin writes", async () => {
    const protectedResponse = await request("/api/workspace/file?path=.env");
    expect(protectedResponse.status).toBe(403);

    const body = JSON.stringify({
      content: "x",
      expected_revision: revision(new Uint8Array()),
      path: "new.txt",
    });
    const originResponse = await request("/api/workspace/file", {
      body,
      headers: {
        "Content-Length": String(Buffer.byteLength(body)),
        "Content-Type": "application/json",
        Origin: "https://untrusted.example",
      },
      method: "PUT",
    });
    expect(originResponse.status).toBe(403);
    expect(await originResponse.json()).toMatchObject({
      error: "origin_not_allowed",
    });
  });

  test("protects nested repository and environment metadata", async () => {
    mkdirSync(join(root, "project", ".git"), { recursive: true });
    mkdirSync(join(root, "project"), { recursive: true });
    writeFileSync(join(root, "project", ".git", "config"), "secret");
    writeFileSync(join(root, "project", ".env.local"), "secret");

    for (const path of ["project/.git/config", "project/.env.local"]) {
      const response = await request(
        `/api/workspace/file?path=${encodeURIComponent(path)}`,
      );
      expect(response.status).toBe(403);
    }
  });

  test("rejects writes to non-editable file types", async () => {
    const body = JSON.stringify({
      content: "#!/bin/sh\n",
      expected_revision: revision(new Uint8Array()),
      path: "script.sh",
    });
    const response = await request("/api/workspace/file", {
      body,
      headers: {
        "Content-Type": "application/json",
      },
      method: "PUT",
    });
    expect(response.status).toBe(415);
    expect(await response.json()).toMatchObject({ error: "not_editable" });
  });

  test("serves the Vite index and assets without exposing other files", async () => {
    const staticRoot = join(root, "static");
    mkdirSync(join(staticRoot, "assets"), { recursive: true });
    writeFileSync(join(staticRoot, "index.html"), "<!doctype html>");
    writeFileSync(join(staticRoot, "assets", "app.js"), "console.log('ok')");
    const staticHandler = createWorkspaceHandler({
      staticRoot,
      workspaceRoot: root,
    });

    const index = await staticHandler(new Request("http://localhost/"));
    const asset = await staticHandler(
      new Request("http://localhost/assets/app.js"),
    );
    const missing = await staticHandler(
      new Request("http://localhost/assets/../note.md"),
    );

    expect(index.status).toBe(200);
    expect(asset.headers.get("content-type")).toBe(
      "text/javascript; charset=utf-8",
    );
    expect(await asset.text()).toContain("console.log");
    expect(missing.status).toBe(404);
  });

  test("protects workspace routes with an Argon2id session", async () => {
    const passwordHash = await Bun.password.hash(
      "correct horse battery staple",
      {
        algorithm: "argon2id",
        memoryCost: 32 * 1024,
        timeCost: 2,
      },
    );
    const authenticatedHandler = createWorkspaceHandler({
      authRequired: true,
      passwordHash,
      workspaceRoot: root,
    });
    const unauthenticated = await authenticatedHandler(
      new Request("http://localhost/api/workspace/tree"),
    );
    expect(unauthenticated.status).toBe(401);

    const wrongLogin = await authenticatedHandler(
      new Request("http://localhost/api/auth/login", {
        body: JSON.stringify({ password: "wrong password" }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      }),
    );
    expect(wrongLogin.status).toBe(401);

    const login = await authenticatedHandler(
      new Request("http://localhost/api/auth/login", {
        body: JSON.stringify({ password: "correct horse battery staple" }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      }),
    );
    expect(login.status).toBe(200);
    const cookie = login.headers.get("set-cookie");
    expect(cookie).toContain("HttpOnly");
    expect(cookie).toContain("SameSite=Strict");
    expect(cookie).not.toContain("Secure");

    const authenticated = await authenticatedHandler(
      new Request("http://localhost/api/workspace/tree", {
        headers: { Cookie: cookie?.split(";")[0] ?? "" },
      }),
    );
    expect(authenticated.status).toBe(200);

    const logout = await authenticatedHandler(
      new Request("http://localhost/api/auth/logout", {
        headers: { Cookie: cookie?.split(";")[0] ?? "" },
        method: "POST",
      }),
    );
    expect(logout.status).toBe(200);
    expect(logout.headers.get("set-cookie")).toContain("Max-Age=0");
    const afterLogout = await authenticatedHandler(
      new Request("http://localhost/api/workspace/tree", {
        headers: { Cookie: cookie?.split(";")[0] ?? "" },
      }),
    );
    expect(afterLogout.status).toBe(401);
  });

  test("accepts an HTTPS origin forwarded to an HTTP upstream", async () => {
    const passwordHash = await Bun.password.hash(
      "correct horse battery staple",
      {
        algorithm: "argon2id",
        memoryCost: 32 * 1024,
        timeCost: 2,
      },
    );
    const proxiedHandler = createWorkspaceHandler({
      authRequired: true,
      passwordHash,
      workspaceRoot: root,
    });
    const login = await proxiedHandler(
      new Request("http://workspace-ui:8089/api/auth/login", {
        body: JSON.stringify({ password: "correct horse battery staple" }),
        headers: {
          "Content-Type": "application/json",
          Origin: "https://workspace.example.test",
          "X-Forwarded-Host": "workspace.example.test",
          "X-Forwarded-Proto": "https",
        },
        method: "POST",
      }),
    );

    expect(login.status).toBe(200);
    expect(login.headers.get("set-cookie")).toContain("Secure");

    const untrusted = await proxiedHandler(
      new Request("http://workspace-ui:8089/api/auth/login", {
        body: JSON.stringify({ password: "correct horse battery staple" }),
        headers: {
          "Content-Type": "application/json",
          Origin: "https://untrusted.example",
          "X-Forwarded-Host": "workspace.example.test",
          "X-Forwarded-Proto": "https",
        },
        method: "POST",
      }),
    );

    expect(untrusted.status).toBe(403);
    expect(await untrusted.json()).toMatchObject({
      error: "origin_not_allowed",
    });
  });

  test("accepts a configured public origin without forwarded headers", async () => {
    const passwordHash = await Bun.password.hash(
      "correct horse battery staple",
      {
        algorithm: "argon2id",
        memoryCost: 32 * 1024,
        timeCost: 2,
      },
    );
    const configuredHandler = createWorkspaceHandler({
      authRequired: true,
      passwordHash,
      publicOrigin: "https://openlia.example.test",
      workspaceRoot: root,
    });
    const login = await configuredHandler(
      new Request("http://workspace-ui:8089/api/auth/login", {
        body: JSON.stringify({ password: "correct horse battery staple" }),
        headers: {
          "Content-Type": "application/json",
          Origin: "https://openlia.example.test",
        },
        method: "POST",
      }),
    );

    expect(login.status).toBe(200);
    expect(login.headers.get("set-cookie")).toContain("Secure");
  });

  test("rejects oversized chunked login requests before password verification", async () => {
    const passwordHash = await Bun.password.hash(
      "correct horse battery staple",
      {
        algorithm: "argon2id",
        memoryCost: 32 * 1024,
        timeCost: 2,
      },
    );
    const authenticatedHandler = createWorkspaceHandler({
      authRequired: true,
      passwordHash,
      workspaceRoot: root,
    });
    const body = new TextEncoder().encode(
      JSON.stringify({ password: "x".repeat(20 * 1024) }),
    );
    const response = await authenticatedHandler(
      new Request("http://localhost/api/auth/login", {
        body: new ReadableStream({
          start(controller) {
            controller.enqueue(body);
            controller.close();
          },
        }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      }),
    );
    expect(response.status).toBe(413);
  });
});
