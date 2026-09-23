export interface WorkspaceFileMetadata {
  path: string;
  size: number;
  modified_at: string;
  editable: boolean;
}

export type WorkspaceTreeEntry =
  | { path: string; kind: "directory" }
  | (WorkspaceFileMetadata & { kind: "file" });

export interface WorkspaceTreeResponse {
  schema: 1;
  entries: WorkspaceTreeEntry[];
  truncated: boolean;
}

export interface WorkspaceFile extends WorkspaceFileMetadata {
  schema: 1;
  content: string;
  revision: string;
}

export interface WorkspaceWriteResponse extends WorkspaceFileMetadata {
  schema: 1;
  ok: true;
  revision: string;
}

export interface WorkspaceGitStatus {
  schema: 1;
  configured: boolean;
  branch?: string;
  dirty?: boolean;
  status?: string;
}

export interface WorkspaceErrorResponse {
  schema: 1;
  ok: false;
  error: string;
  current_revision?: string;
}

export interface WorkspaceWriteRequest {
  path: string;
  content: string;
  expected_revision: string;
}

export interface AuthSessionResponse {
  schema: 1;
  auth_required: boolean;
  authenticated: boolean;
}

export interface AuthLoginResponse {
  schema: 1;
  ok: true;
}
