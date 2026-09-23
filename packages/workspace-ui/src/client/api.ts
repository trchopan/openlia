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

export class ApiError extends Error {
  readonly status: number;
  readonly payload: WorkspaceErrorResponse | null;

  constructor(status: number, payload: WorkspaceErrorResponse | null) {
    super(payload?.error ?? "request_failed");
    this.name = "ApiError";
    this.status = status;
    this.payload = payload;
  }
}

export interface WorkspaceApi {
  loadTree(): Promise<WorkspaceTreeResponse>;
  loadSession(): Promise<AuthSessionResponse>;
  login(password: string): Promise<AuthLoginResponse>;
  logout(): Promise<AuthLoginResponse>;
  loadFile(path: string): Promise<WorkspaceFile>;
  saveFile(
    path: string,
    content: string,
    expectedRevision: string,
  ): Promise<WorkspaceWriteResponse>;
  loadGitStatus(): Promise<WorkspaceGitStatus>;
  downloadUrl(path: string): string;
}

type ResponseValidator<T> = (value: unknown) => value is T;

async function requestJson<T>(
  input: string,
  validator: ResponseValidator<T>,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(input, init);
  const payload: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    throw new ApiError(
      response.status,
      isErrorResponse(payload) ? payload : null,
    );
  }
  if (!validator(payload)) throw new Error("invalid_api_response");
  return payload;
}

function isErrorResponse(value: unknown): value is WorkspaceErrorResponse {
  return (
    typeof value === "object" &&
    value !== null &&
    "schema" in value &&
    value.schema === 1 &&
    "ok" in value &&
    value.ok === false &&
    "error" in value &&
    typeof value.error === "string"
  );
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isWorkspaceFileMetadata(
  value: unknown,
): value is WorkspaceFileMetadata {
  return (
    isObject(value) &&
    typeof value.path === "string" &&
    typeof value.size === "number" &&
    Number.isFinite(value.size) &&
    typeof value.modified_at === "string" &&
    typeof value.editable === "boolean"
  );
}

function isWorkspaceTreeEntry(value: unknown): value is WorkspaceTreeEntry {
  return (
    isObject(value) &&
    typeof value.path === "string" &&
    (value.kind === "directory" ||
      (value.kind === "file" && isWorkspaceFileMetadata(value)))
  );
}

function isWorkspaceTreeResponse(
  value: unknown,
): value is WorkspaceTreeResponse {
  return (
    isObject(value) &&
    value.schema === 1 &&
    Array.isArray(value.entries) &&
    value.entries.every(isWorkspaceTreeEntry) &&
    typeof value.truncated === "boolean"
  );
}

function isAuthSessionResponse(value: unknown): value is AuthSessionResponse {
  return (
    isObject(value) &&
    value.schema === 1 &&
    typeof value.auth_required === "boolean" &&
    typeof value.authenticated === "boolean"
  );
}

function isAuthLoginResponse(value: unknown): value is AuthLoginResponse {
  return isObject(value) && value.schema === 1 && value.ok === true;
}

function isWorkspaceFile(value: unknown): value is WorkspaceFile {
  return (
    isWorkspaceFileMetadata(value) &&
    "schema" in value &&
    value.schema === 1 &&
    "content" in value &&
    "revision" in value &&
    typeof value.content === "string" &&
    typeof value.revision === "string"
  );
}

function isWorkspaceWriteResponse(
  value: unknown,
): value is WorkspaceWriteResponse {
  return (
    isWorkspaceFileMetadata(value) &&
    "schema" in value &&
    value.schema === 1 &&
    "ok" in value &&
    "revision" in value &&
    value.ok === true &&
    typeof value.revision === "string"
  );
}

function isWorkspaceGitStatus(value: unknown): value is WorkspaceGitStatus {
  return (
    isObject(value) &&
    value.schema === 1 &&
    typeof value.configured === "boolean" &&
    (value.branch === undefined || typeof value.branch === "string") &&
    (value.dirty === undefined || typeof value.dirty === "boolean") &&
    (value.status === undefined || typeof value.status === "string")
  );
}

export const httpWorkspaceApi: WorkspaceApi = {
  loadTree: () =>
    requestJson<WorkspaceTreeResponse>(
      "/api/workspace/tree",
      isWorkspaceTreeResponse,
    ),
  loadSession: () =>
    requestJson<AuthSessionResponse>(
      "/api/auth/session",
      isAuthSessionResponse,
    ),
  login: (password) =>
    requestJson<AuthLoginResponse>("/api/auth/login", isAuthLoginResponse, {
      body: JSON.stringify({ password }),
      headers: { "Content-Type": "application/json" },
      method: "POST",
    }),
  logout: () =>
    requestJson<AuthLoginResponse>("/api/auth/logout", isAuthLoginResponse, {
      headers: { "Content-Type": "application/json" },
      method: "POST",
    }),
  loadFile: (path) =>
    requestJson<WorkspaceFile>(
      `/api/workspace/file?${new URLSearchParams({ path })}`,
      isWorkspaceFile,
    ),
  saveFile: (path, content, expectedRevision) =>
    requestJson<WorkspaceWriteResponse>(
      "/api/workspace/file",
      isWorkspaceWriteResponse,
      {
        body: JSON.stringify({
          content,
          expected_revision: expectedRevision,
          path,
        }),
        headers: { "Content-Type": "application/json" },
        method: "PUT",
      },
    ),
  loadGitStatus: () =>
    requestJson<WorkspaceGitStatus>(
      "/api/workspace/git/status",
      isWorkspaceGitStatus,
    ),
  downloadUrl: (path) =>
    `/api/workspace/download?${new URLSearchParams({ path })}`,
};
