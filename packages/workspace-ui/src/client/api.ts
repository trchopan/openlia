import type {
  AuthSessionResponse,
  WorkspaceErrorResponse,
  WorkspaceFile,
  WorkspaceGitStatus,
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

async function requestJson<T>(input: string, init?: RequestInit): Promise<T> {
  const response = await fetch(input, init);
  const payload: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    throw new ApiError(
      response.status,
      isErrorResponse(payload) ? payload : null,
    );
  }
  return payload as T;
}

function isErrorResponse(value: unknown): value is WorkspaceErrorResponse {
  return (
    typeof value === "object" &&
    value !== null &&
    "ok" in value &&
    value.ok === false &&
    "error" in value
  );
}

export function loadTree(): Promise<WorkspaceTreeResponse> {
  return requestJson<WorkspaceTreeResponse>("/api/workspace/tree");
}

export function loadSession(): Promise<AuthSessionResponse> {
  return requestJson<AuthSessionResponse>("/api/auth/session");
}

export function login(password: string): Promise<void> {
  return requestJson<void>("/api/auth/login", {
    body: JSON.stringify({ password }),
    headers: { "Content-Type": "application/json" },
    method: "POST",
  });
}

export function logout(): Promise<void> {
  return requestJson<void>("/api/auth/logout", {
    headers: { "Content-Type": "application/json" },
    method: "POST",
  });
}

export function loadFile(path: string): Promise<WorkspaceFile> {
  return requestJson<WorkspaceFile>(
    `/api/workspace/file?${new URLSearchParams({ path })}`,
  );
}

export function saveFile(
  path: string,
  content: string,
  expectedRevision: string,
): Promise<WorkspaceWriteResponse> {
  return requestJson<WorkspaceWriteResponse>("/api/workspace/file", {
    body: JSON.stringify({
      content,
      expected_revision: expectedRevision,
      path,
    }),
    headers: { "Content-Type": "application/json" },
    method: "PUT",
  });
}

export function loadGitStatus(): Promise<WorkspaceGitStatus> {
  return requestJson<WorkspaceGitStatus>("/api/workspace/git/status");
}

export function downloadUrl(path: string): string {
  return `/api/workspace/download?${new URLSearchParams({ path })}`;
}
