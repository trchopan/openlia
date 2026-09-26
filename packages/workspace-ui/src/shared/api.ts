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

export interface SkillPrerequisites {
  env_vars?: string[] | undefined;
  credential_files?: string[] | undefined;
}

export interface SkillSummary {
  id: string;
  name: string;
  category: string;
  description: string;
  version?: string | undefined;
  author?: string | undefined;
  tags: string[];
  enabled: boolean;
  pinned: boolean;
  bundled: boolean;
  useCount: number;
  lastUsedAt?: string | undefined;
  fileCount: number;
}

export interface SkillFileEntry {
  path: string;
  size: number;
  modified_at: string;
  editable: boolean;
}

export interface SkillDetail extends SkillSummary {
  license?: string | undefined;
  platforms?: string[] | undefined;
  prerequisites?: SkillPrerequisites | undefined;
  files: SkillFileEntry[];
}

export interface SkillsListResponse {
  schema: 1;
  skills: SkillSummary[];
  categories: string[];
}

export interface SkillDetailResponse {
  schema: 1;
  skill: SkillDetail;
}

export interface SkillFileResponse {
  schema: 1;
  id: string;
  path: string;
  content: string;
  revision: string;
  editable: boolean;
  size: number;
  modified_at: string;
}

export interface SkillWriteRequest {
  id: string;
  path: string;
  content: string;
  expected_revision: string;
}

export interface SkillWriteResponse {
  schema: 1;
  ok: true;
  id: string;
  path: string;
  revision: string;
  size: number;
  modified_at: string;
}

export interface SkillToggleEnableRequest {
  id: string;
  enabled: boolean;
}

export interface SkillTogglePinRequest {
  id: string;
  pinned: boolean;
}

export interface SkillCreateRequest {
  name: string;
  category?: string | undefined;
  description: string;
}

export interface SkillSuccessResponse {
  schema: 1;
  ok: true;
  id?: string | undefined;
}
