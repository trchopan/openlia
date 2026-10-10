import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createWorkspaceHandler } from "./app";

let root = "";
let ingestionRoot = "";
let handler: ReturnType<typeof createWorkspaceHandler>;

function request(path: string): Promise<Response> {
  return handler(new Request(`http://localhost${path}`));
}

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "openlia-ingestion-ui-"));
  ingestionRoot = mkdtempSync(join(tmpdir(), "openlia-ingestion-runtime-"));
  mkdirSync(join(root, "inbox", "ingestion"), { recursive: true });
  mkdirSync(join(root, "sources", "records"), { recursive: true });
  mkdirSync(
    join(
      root,
      "sources",
      "artifacts",
      "src_1",
      "ver_1",
      "extractions",
      "ext_1",
    ),
    { recursive: true },
  );
  writeFileSync(
    join(root, "inbox", "ingestion", "intake_1.md"),
    `---
intake_id: intake_1
source_id: src_1
version_id: ver_1
captured_at: "2026-10-10T10:00:00Z"
kind: pdf
operation: extract
requested_outputs:
  - text
status: processing
callback:
  event_id: evt_1
  delivery_status: pending
---
# Intake
`,
  );
  writeFileSync(
    join(root, "sources", "records", "src_1.md"),
    `---
id: src_1
kind: pdf
title: Quarterly statement
status: captured
captured_at: "2026-10-10T10:00:00Z"
origin:
  channel: chat
versions:
  - id: ver_1
    original_artifact_id: art_original
    original_ref: sources/artifacts/src_1/ver_1/original.pdf
    extractions:
      - ext_1
---
# Source
`,
  );
  writeFileSync(
    join(root, "sources", "artifacts", "src_1", "ver_1", "original.pdf"),
    "original",
  );
  writeFileSync(
    join(
      root,
      "sources",
      "artifacts",
      "src_1",
      "ver_1",
      "extractions",
      "ext_1",
      "text.md",
    ),
    "extracted",
  );
  writeFileSync(
    join(
      root,
      "sources",
      "artifacts",
      "src_1",
      "ver_1",
      "extractions",
      "ext_1",
      "manifest.json",
    ),
    JSON.stringify({
      artifact_id: "art_text",
      role: "text",
      complete: true,
      warnings: [],
    }),
  );
  writeFileSync(
    join(ingestionRoot, "status.json"),
    JSON.stringify({
      schema: 1,
      generated_at: Date.now() / 1000,
      instance_id: "runtime_1",
      job_counts: { running: 1 },
      jobs: [
        {
          job_id: "job_1",
          intake_id: "intake_1",
          source_id: "src_1",
          version_id: "ver_1",
          operation: "extract",
          requested_outputs: ["text"],
          state: "running",
          attempts: 1,
          error_code: null,
          error_summary: null,
          heartbeat_at: Date.now() / 1000,
          lease_expires_at: Date.now() / 1000 + 90,
          next_attempt_at: null,
          created_at: Date.now() / 1000,
          updated_at: Date.now() / 1000,
        },
      ],
      events: [
        {
          event_id: "evt_1",
          job_id: "job_1",
          event_type: "ingestion.completed",
          delivery_state: "awaiting_ack",
          attempts: 1,
          last_error: null,
          delivered_at: Date.now() / 1000,
          acknowledged_at: null,
          created_at: Date.now() / 1000,
          updated_at: Date.now() / 1000,
        },
      ],
      actions: [],
    }),
  );
  handler = createWorkspaceHandler({ ingestionRoot, workspaceRoot: root });
});

afterEach(() => {
  rmSync(root, { force: true, recursive: true });
  rmSync(ingestionRoot, { force: true, recursive: true });
});

describe("ingestion HTTP endpoints", () => {
  test("joins durable intake records with live runtime state", async () => {
    const response = await request("/api/workspace/ingestion");
    expect(response.status).toBe(200);
    const payload = (await response.json()) as {
      live_available: boolean;
      intakes: Array<Record<string, unknown>>;
    };
    expect(payload.live_available).toBe(true);
    expect(payload.intakes[0]).toMatchObject({
      intake_id: "intake_1",
      source_title: "Quarterly statement",
      status: "processing",
      job: { state: "running", job_id: "job_1" },
      callback: { delivery_state: "awaiting_ack" },
    });
  });

  test("returns source artifacts and records for an intake detail", async () => {
    const response = await request("/api/workspace/ingestion/intake_1");
    expect(response.status).toBe(200);
    const payload = (await response.json()) as {
      source: { title: string };
      artifacts: Array<{ path: string }>;
    };
    expect(payload.source.title).toBe("Quarterly statement");
    expect(
      payload.artifacts.map((item: { path: string }) => item.path),
    ).toEqual([
      "sources/artifacts/src_1/ver_1/original.pdf",
      "sources/artifacts/src_1/ver_1/extractions/ext_1/text.md",
    ]);
  });

  test("falls back to durable records when runtime state is unavailable", async () => {
    rmSync(join(ingestionRoot, "status.json"));
    const response = await request("/api/workspace/ingestion");
    const payload = (await response.json()) as {
      live_available: boolean;
      intakes: Array<{ status: string }>;
    };
    expect(payload.live_available).toBe(false);
    expect(payload.intakes[0]?.status).toBe("processing");
  });
});
