import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { WorkspaceActivityResponse } from "../shared/api";
import { createWorkspaceHandler } from "./app";
import { WorkspaceService } from "./workspace";

let root = "";
let handler: ReturnType<typeof createWorkspaceHandler>;

function request(path: string, init?: RequestInit): Promise<Response> {
  return handler(new Request(`http://localhost${path}`, init));
}

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "openlia-workspace-activity-"));
  handler = createWorkspaceHandler({ workspaceRoot: root });
});

afterEach(() => {
  rmSync(root, { force: true, recursive: true });
});

describe("workspace activity", () => {
  test("returns recent files when git is not configured", async () => {
    writeFileSync(join(root, "notes.md"), "# Notes\nContent");
    writeFileSync(join(root, "tasks.md"), "# Tasks\nTodo");

    const response = await request("/api/workspace/activity");
    expect(response.status).toBe(200);

    const payload = (await response.json()) as WorkspaceActivityResponse;
    expect(payload.schema).toBe(1);
    expect(payload.gitConfigured).toBe(false);
    expect(payload.commits).toEqual([]);
    expect(payload.uncommitted).toEqual([]);
    expect(payload.recentFiles.length).toBe(2);
    expect(payload.recentFiles.map((f) => f.path)).toContain("notes.md");
    expect(payload.recentFiles.map((f) => f.path)).toContain("tasks.md");
  });

  test("returns uncommitted changes and commits when git is configured", async () => {
    Bun.spawnSync(["git", "init", "-b", "main"], { cwd: root });
    Bun.spawnSync(["git", "config", "user.name", "Tester"], { cwd: root });
    Bun.spawnSync(["git", "config", "user.email", "test@example.com"], {
      cwd: root,
    });

    writeFileSync(join(root, "initial.md"), "# Initial");
    Bun.spawnSync(["git", "add", "initial.md"], { cwd: root });
    Bun.spawnSync(["git", "commit", "-m", "feat: initial commit"], {
      cwd: root,
    });

    writeFileSync(join(root, "second.md"), "# Second");
    Bun.spawnSync(["git", "add", "second.md"], { cwd: root });
    Bun.spawnSync(["git", "commit", "-m", "docs: second commit"], {
      cwd: root,
    });

    // Create an uncommitted modified file
    writeFileSync(join(root, "initial.md"), "# Initial modified");
    // Create an uncommitted untracked file
    writeFileSync(join(root, "untracked.md"), "# Untracked");

    const response = await request("/api/workspace/activity?limit=5");
    expect(response.status).toBe(200);

    const payload = (await response.json()) as WorkspaceActivityResponse;
    expect(payload.schema).toBe(1);
    expect(payload.gitConfigured).toBe(true);
    expect(payload.branch).toBe("main");
    expect(payload.commits.length).toBe(2);
    expect(payload.commits[0]?.message).toBe("docs: second commit");
    expect(payload.commits[0]?.author).toBe("Tester");
    expect(payload.commits[0]?.files[0]).toEqual({
      path: "second.md",
      status: "added",
    });
    expect(payload.commits[1]?.message).toBe("feat: initial commit");

    // Check uncommitted
    const uncommittedPaths = payload.uncommitted.map((u) => u.path);
    expect(uncommittedPaths).toContain("initial.md");
    expect(uncommittedPaths).toContain("untracked.md");

    // Check recent files
    expect(payload.recentFiles.length).toBeGreaterThanOrEqual(2);
  });

  test("excludes protected paths from activity", async () => {
    Bun.spawnSync(["git", "init", "-b", "main"], { cwd: root });
    Bun.spawnSync(["git", "config", "user.name", "Tester"], { cwd: root });
    Bun.spawnSync(["git", "config", "user.email", "test@example.com"], {
      cwd: root,
    });

    writeFileSync(join(root, "notes.md"), "# Notes");
    writeFileSync(join(root, ".env"), "SECRET=true");
    Bun.spawnSync(["git", "add", "-A"], { cwd: root });
    Bun.spawnSync(["git", "commit", "-m", "chore: setup"], { cwd: root });

    const service = new WorkspaceService({ workspaceRoot: root });
    const activity = await service.activity();

    const commitFiles = activity.commits.flatMap((c) =>
      c.files.map((f) => f.path),
    );
    expect(commitFiles).toContain("notes.md");
    expect(commitFiles).not.toContain(".env");

    const recentFilePaths = activity.recentFiles.map((f) => f.path);
    expect(recentFilePaths).toContain("notes.md");
    expect(recentFilePaths).not.toContain(".env");
  });
});
