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

export interface WorkspaceFileValidation {
  valid: boolean;
  schema_path?: string;
  errors: string[];
}

export interface WorkspaceFile extends WorkspaceFileMetadata {
  schema: 1;
  content: string;
  revision: string;
  validation?: WorkspaceFileValidation;
}

export interface WorkspaceWriteResponse extends WorkspaceFileMetadata {
  schema: 1;
  ok: true;
  revision: string;
  validation?: WorkspaceFileValidation;
}

export interface WorkspaceDeleteResponse {
  schema: 1;
  ok: true;
  path: string;
}

export interface WorkspaceMoveResponse extends WorkspaceFileMetadata {
  schema: 1;
  ok: true;
  previous_path: string;
  revision: string;
  validation?: WorkspaceFileValidation;
}

export interface WorkspaceRenameResponse extends WorkspaceFileMetadata {
  schema: 1;
  ok: true;
  previous_path: string;
  revision: string;
  validation?: WorkspaceFileValidation;
}

export interface WorkspaceGitStatus {
  schema: 1;
  configured: boolean;
  branch?: string;
  dirty?: boolean;
  status?: string;
}

export interface WorkspaceCommitChange {
  path: string;
  status: "added" | "modified" | "deleted" | "renamed";
}

export interface WorkspaceCommit {
  hash: string;
  shortHash: string;
  author: string;
  message: string;
  timestamp: string;
  files: WorkspaceCommitChange[];
}

export interface WorkspaceUncommittedChange {
  path: string;
  status: "added" | "modified" | "deleted" | "untracked";
}

export interface WorkspaceActivityResponse {
  schema: 1;
  gitConfigured: boolean;
  branch?: string | undefined;
  uncommitted: WorkspaceUncommittedChange[];
  commits: WorkspaceCommit[];
  recentFiles: WorkspaceFileMetadata[];
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

export interface WorkspaceDeleteRequest {
  path: string;
  expected_revision: string;
}

export interface WorkspaceMoveRequest {
  source_path: string;
  destination_path: string;
  expected_revision: string;
}

export interface WorkspaceRenameRequest {
  path: string;
  new_name: string;
  expected_revision: string;
}

export interface WorkspaceDiagnosticIssue {
  path: string;
  schema_path?: string | undefined;
  errors: string[];
}

export interface WorkspaceDiagnosticsResponse {
  schema: 1;
  valid: boolean;
  issues: WorkspaceDiagnosticIssue[];
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
