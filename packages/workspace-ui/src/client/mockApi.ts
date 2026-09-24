import type {
  AuthLoginResponse,
  AuthSessionResponse,
  WorkspaceErrorResponse,
  WorkspaceFile,
  WorkspaceFileMetadata,
  WorkspaceGitStatus,
  WorkspaceTreeEntry,
  WorkspaceTreeResponse,
  WorkspaceWriteResponse,
} from "../shared/api";
import { ApiError, type WorkspaceApi } from "./api";

export type MockWorkspaceScenario =
  | "default"
  | "auth"
  | "conflict"
  | "empty"
  | "error"
  | "loading";

export interface MockWorkspaceOptions {
  scenario?: MockWorkspaceScenario;
}

const modifiedAt = "2026-09-22T00:00:00.000Z";

function error(
  status: number,
  code: string,
  currentRevision?: string,
): ApiError {
  const payload: WorkspaceErrorResponse = {
    schema: 1,
    ok: false,
    error: code,
  };
  if (currentRevision !== undefined) payload.current_revision = currentRevision;
  return new ApiError(status, payload);
}

function metadata(path: string, content: string): WorkspaceFileMetadata {
  return {
    editable: true,
    modified_at: modifiedAt,
    path,
    size: new TextEncoder().encode(content).byteLength,
  };
}

export function createMockWorkspaceApi({
  scenario = "default",
}: MockWorkspaceOptions = {}): WorkspaceApi {
  const path = "notes.md";
  let content = "Hello";
  let revision = "sha256:mock-before";
  let saveCount = 0;
  let authenticated = scenario !== "auth";
  let conflictPending = scenario === "conflict";
  const entries: WorkspaceTreeEntry[] = [
    { kind: "directory", path: "calendar" },
    {
      ...metadata(
        "calendar/event.md",
        "# Calendar event\n\nA sample event for UI development.\n",
      ),
      kind: "file",
    },
    { kind: "directory", path: "projects" },
    {
      ...metadata("projects/project.md", "# Project\n\nA sample project.\n"),
      kind: "file",
    },
    { kind: "directory", path: "tasks" },
    {
      ...metadata("tasks/task.md", "# Task\n\nA sample task.\n"),
      kind: "file",
    },
    { ...metadata(path, content), kind: "file" },
  ];

  const fileContents: Record<string, string> = {
    "calendar/event.md":
      "# Calendar event\n\nA sample event for UI development.\n",
    "projects/project.md": "# Project\n\nA sample project.\n",
    "tasks/task.md": "# Task\n\nA sample task.\n",
    [path]: content,
  };
  const fileRevisions: Record<string, string> = {
    "calendar/event.md": "sha256:mock-calendar",
    "projects/project.md": "sha256:mock-project",
    "tasks/task.md": "sha256:mock-task",
    [path]: revision,
  };

  function requireAuthentication() {
    if (!authenticated) throw error(401, "authentication_required");
  }

  function fileResponse(filePath: string): WorkspaceFile {
    const current = fileContents[filePath];
    if (current === undefined) throw error(404, "not_found");
    return {
      ...metadata(filePath, current),
      content: current,
      revision: fileRevisions[filePath] ?? revision,
      schema: 1,
    };
  }

  function treeResponse(): WorkspaceTreeResponse {
    return {
      entries,
      schema: 1,
      truncated: false,
    };
  }

  return {
    async loadTree() {
      requireAuthentication();
      return scenario === "empty"
        ? { ...treeResponse(), entries: [] }
        : treeResponse();
    },
    async loadSession(): Promise<AuthSessionResponse> {
      if (scenario === "error") throw error(500, "request_failed");
      if (scenario === "loading") await new Promise<void>(() => undefined);
      return {
        auth_required: scenario === "auth",
        authenticated,
        schema: 1,
      };
    },
    async login(password): Promise<AuthLoginResponse> {
      if (!password) throw error(401, "invalid_login");
      authenticated = true;
      return { ok: true, schema: 1 };
    },
    async logout(): Promise<AuthLoginResponse> {
      authenticated = false;
      return { ok: true, schema: 1 };
    },
    async loadFile(requestedPath) {
      requireAuthentication();
      return fileResponse(requestedPath);
    },
    async saveFile(requestedPath, nextContent, expectedRevision) {
      requireAuthentication();
      if (!(requestedPath in fileContents)) throw error(404, "not_found");
      if (requestedPath === path && conflictPending) {
        conflictPending = false;
        throw error(409, "revision_conflict", "sha256:mock-current");
      }
      const currentRev = fileRevisions[requestedPath] ?? revision;
      if (expectedRevision !== currentRev)
        throw error(409, "revision_conflict", currentRev);
      fileContents[requestedPath] = nextContent;
      if (requestedPath === path) content = nextContent;
      const nextRev = `sha256:mock-${++saveCount}`;
      fileRevisions[requestedPath] = nextRev;
      if (requestedPath === path) revision = nextRev;
      const response: WorkspaceWriteResponse = {
        ...metadata(requestedPath, nextContent),
        ok: true,
        revision: nextRev,
        schema: 1,
      };
      return response;
    },
    async loadGitStatus(): Promise<WorkspaceGitStatus> {
      requireAuthentication();
      return {
        branch: "main",
        configured: true,
        dirty: fileContents[path] !== "Hello",
        schema: 1,
      };
    },
    downloadUrl(requestedPath) {
      const current = fileContents[requestedPath];
      if (current === undefined) return "#";
      return `data:text/plain;charset=utf-8,${encodeURIComponent(current)}`;
    },
  };
}
