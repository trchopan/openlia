#!/usr/bin/env bun
/** Submit and inspect OpenLia browser jobs through the private Bun service. */

import { mkdir, rename } from "node:fs/promises";
import { dirname, join } from "node:path";

const baseUrl = (process.env.OPENLIA_BROWSER_JOBS_URL ?? "http://browser-tools:8932").replace(/\/+$/, "");
const clientId = process.env.OPENLIA_BROWSER_CLIENT_ID ?? "default";
const languagePattern = /^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$/;

async function request(method: string, path: string, body?: Record<string, unknown>): Promise<{ status: number; type: string; data: Uint8Array }> {
  const init: RequestInit = { method, headers: { Accept: "application/json", "X-OpenLia-Client-Id": clientId }, signal: AbortSignal.timeout(15_000) };
  if (body) {
    init.body = JSON.stringify(body);
    (init.headers as Record<string, string>)["Content-Type"] = "application/json";
  }
  const response = await fetch(baseUrl + path, init);
  return { status: response.status, type: response.headers.get("content-type") ?? "", data: new Uint8Array(await response.arrayBuffer()) };
}

function value(args: string[], name: string, fallback = ""): string {
  const index = args.indexOf(name);
  if (index < 0) return fallback;
  const item = args[index + 1];
  if (!item) throw new Error(`${name} requires a value`);
  return item;
}

function timeoutValue(args: string[]): number {
  const raw = value(args, "--timeout-seconds", "300");
  if (!/^\d+$/.test(raw.trim()))
    throw new Error("--timeout-seconds requires a non-negative integer");
  return Number(raw);
}

function outputLanguageValue(args: string[]): string {
  const language = value(
    args,
    "--language",
    process.env.OPENLIA_OUTPUT_LANGUAGE ?? "en",
  ).trim();
  if (!languagePattern.test(language))
    throw new Error("--language must be a BCP 47 language tag");
  return language;
}

async function printResult(result: { type: string; data: Uint8Array }): Promise<void> {
  if (result.type.includes("application/json")) {
    const text = new TextDecoder().decode(result.data);
    try { console.log(JSON.stringify(JSON.parse(text), null, 2)); } catch { console.log(text); }
  } else {
    process.stdout.write(result.data);
    if (result.data.length && result.data.at(-1) !== 10) process.stdout.write("\n");
  }
}

function workspaceRoot(): string {
  return process.env.OPENLIA_WORKSPACE_ROOT ?? join(process.env.HERMES_HOME ?? "/opt/data", "workspace");
}

async function persistResult(
  job: Record<string, unknown>,
  data: Uint8Array,
): Promise<string> {
  const tool = String(job.tool ?? "");
  const platform = tool === "chatgpt-chat" ? "chatgpt" : tool === "gemini-chat" ? "gemini" : "travel";
  const resultName = String(job.result_name ?? `${String(job.job_id)}.yaml`).replace(/[^A-Za-z0-9._-]/g, "_");
  const destination = join(workspaceRoot(), platform === "travel" ? "travel" : `knowledge/${platform}`, resultName);
  await mkdir(dirname(destination), { recursive: true, mode: 0o700 });
  const temporary = `${destination}.tmp-${crypto.randomUUID()}`;
  await Bun.write(temporary, data);
  await rename(temporary, destination);
  return destination;
}

async function main(): Promise<void> {
  const args = Bun.argv.slice(2);
  if (args[0] === "--self-test") {
    if (!baseUrl.startsWith("http")) throw new Error("OPENLIA_BROWSER_JOBS_URL must be an HTTP URL");
    console.log("ok");
    return;
  }
  if (args[0] === "--verify") {
    const result = await request("GET", "/health");
    await printResult(result);
    if (result.status < 200 || result.status >= 300)
      throw new Error("OpenLia browser-job service health check failed");
    return;
  }
  const action = args[0];
  if (action === "submit") {
    const tool = args[1];
    if (tool !== "chatgpt-chat" && tool !== "gemini-chat") throw new Error("submit requires chatgpt-chat or gemini-chat");
    const result = await request("POST", `/openlia/tools/${tool}`, { prompt: value(args, "--prompt"), language: outputLanguageValue(args), topic: value(args, "--topic"), idempotency_key: value(args, "--idempotency-key"), timeout_seconds: timeoutValue(args) });
    await printResult(result);
    process.exit(result.status >= 200 && result.status < 300 ? 0 : 1);
  } else if (action === "status" || action === "result" || action === "cancel") {
    const id = args[1];
    if (!id) throw new Error(`${action} requires a job ID`);
    const suffix = action === "result" ? "/result" : action === "cancel" ? "/cancel" : "";
    let statusData: Record<string, unknown> | undefined;
    if (action === "result") {
      const status = await request("GET", `/openlia/jobs/${encodeURIComponent(id)}`);
      if (status.type.includes("application/json")) {
        try { statusData = JSON.parse(new TextDecoder().decode(status.data)) as Record<string, unknown>; } catch { statusData = undefined; }
      }
    }
    const result = await request(action === "cancel" ? "POST" : "GET", `/openlia/jobs/${encodeURIComponent(id)}${suffix}`, action === "cancel" ? {} : undefined);
    if (action === "result" && result.status >= 200 && result.status < 300 && statusData?.ok === true) {
      const path = await persistResult(statusData, result.data);
      console.error(`saved result: ${path}`);
    }
    await printResult(result);
    process.exit(result.status >= 200 && result.status < 300 ? 0 : 1);
  } else {
    throw new Error("usage: openlia_job.ts submit|status|result|cancel ...");
  }
}

main().catch((error: unknown) => { console.error(error instanceof Error ? error.message : String(error)); process.exit(1); });
