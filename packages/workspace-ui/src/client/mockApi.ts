import type {
  AuthLoginResponse,
  AuthSessionResponse,
  WorkspaceErrorResponse,
  WorkspaceFile,
  WorkspaceFileMetadata,
  WorkspaceGitStatus,
  WorkspaceTreeResponse,
  WorkspaceWriteResponse,
} from "../shared/api";
import { ApiError, type WorkspaceApi } from "./api";

export type MockWorkspaceScenario = "default" | "auth" | "conflict";

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

  function requireAuthentication() {
    if (!authenticated) throw error(401, "authentication_required");
  }

  function fileResponse(): WorkspaceFile {
    return {
      ...metadata(path, content),
      content,
      revision,
      schema: 1,
    };
  }

  function treeResponse(): WorkspaceTreeResponse {
    return {
      entries: [{ ...metadata(path, content), kind: "file" }],
      schema: 1,
      truncated: false,
    };
  }

  return {
    async loadTree() {
      requireAuthentication();
      return treeResponse();
    },
    async loadSession(): Promise<AuthSessionResponse> {
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
      if (requestedPath !== path) throw error(404, "not_found");
      return fileResponse();
    },
    async saveFile(requestedPath, nextContent, expectedRevision) {
      requireAuthentication();
      if (requestedPath !== path) throw error(404, "not_found");
      if (conflictPending) {
        conflictPending = false;
        throw error(409, "revision_conflict", "sha256:mock-current");
      }
      if (expectedRevision !== revision)
        throw error(409, "revision_conflict", revision);
      content = nextContent;
      revision = `sha256:mock-${++saveCount}`;
      const response: WorkspaceWriteResponse = {
        ...metadata(path, content),
        ok: true,
        revision,
        schema: 1,
      };
      return response;
    },
    async loadGitStatus(): Promise<WorkspaceGitStatus> {
      requireAuthentication();
      return {
        branch: "main",
        configured: true,
        dirty: content !== "Hello",
        schema: 1,
      };
    },
    downloadUrl(requestedPath) {
      if (requestedPath !== path) return "#";
      return `data:text/plain;charset=utf-8,${encodeURIComponent(content)}`;
    },
  };
}
