import type {
  AuthLoginResponse,
  AuthSessionResponse,
  SkillDetail,
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

  const mockSkills: SkillDetail[] = [
    {
      author: undefined,
      bundled: true,
      category: "productivity",
      description: "Review durable personal claims with explicit evidence.",
      enabled: true,
      fileCount: 2,
      files: [
        {
          editable: true,
          modified_at: modifiedAt,
          path: "SKILL.md",
          size: 450,
        },
        {
          editable: true,
          modified_at: modifiedAt,
          path: "scripts/review.py",
          size: 120,
        },
      ],
      id: "claim-review",
      lastUsedAt: "2026-09-24T00:00:00.000Z",
      name: "claim-review",
      pinned: true,
      platforms: ["macos", "linux"],
      prerequisites: { env_vars: ["CLAIM_API_KEY"] },
      tags: ["claims", "memory"],
      useCount: 14,
      version: "0.1.0",
    },
    {
      author: undefined,
      bundled: true,
      category: "research",
      description: "Exhaustive multi-source research investigation.",
      enabled: true,
      fileCount: 2,
      files: [
        {
          editable: true,
          modified_at: modifiedAt,
          path: "SKILL.md",
          size: 680,
        },
        {
          editable: true,
          modified_at: modifiedAt,
          path: "templates/report.md",
          size: 240,
        },
      ],
      id: "deep-research",
      lastUsedAt: "2026-09-25T12:00:00.000Z",
      name: "deep-research",
      pinned: true,
      platforms: ["macos", "linux"],
      tags: ["research", "web"],
      useCount: 32,
      version: "1.0.0",
    },
    {
      author: "community",
      bundled: false,
      category: "productivity",
      description: "Notion API + ntn CLI: pages, databases, markdown.",
      enabled: true,
      fileCount: 1,
      files: [
        {
          editable: true,
          modified_at: modifiedAt,
          path: "SKILL.md",
          size: 1200,
        },
      ],
      id: "productivity/notion",
      lastUsedAt: "2026-09-26T08:00:00.000Z",
      license: "MIT",
      name: "notion",
      pinned: false,
      platforms: ["linux", "macos", "windows"],
      prerequisites: { env_vars: ["NOTION_API_KEY"] },
      tags: ["Notion", "Productivity", "Notes"],
      useCount: 45,
      version: "2.0.0",
    },
    {
      author: undefined,
      bundled: true,
      category: "creative",
      description: "Generate polished web mockups and design specs.",
      enabled: true,
      fileCount: 1,
      files: [
        {
          editable: true,
          modified_at: modifiedAt,
          path: "SKILL.md",
          size: 520,
        },
      ],
      id: "creative/claude-design",
      lastUsedAt: "2026-09-23T15:30:00.000Z",
      name: "claude-design",
      pinned: false,
      tags: ["design", "ui"],
      useCount: 19,
      version: "1.1.0",
    },
    {
      author: undefined,
      bundled: true,
      category: "apple",
      description: "Apple Notes extraction and synchronization.",
      enabled: false,
      fileCount: 1,
      files: [
        {
          editable: true,
          modified_at: modifiedAt,
          path: "SKILL.md",
          size: 380,
        },
      ],
      id: "apple/apple-notes",
      name: "apple-notes",
      pinned: false,
      platforms: ["macos"],
      tags: ["apple", "notes"],
      useCount: 3,
      version: "0.9.0",
    },
  ];

  const mockSkillFiles: Record<string, string> = {
    "claim-review:SKILL.md": `---\nname: claim-review\ndescription: Review durable personal claims with explicit evidence.\nversion: 0.1.0\nplatforms: [macos, linux]\nprerequisites:\n  env_vars: [CLAIM_API_KEY]\nmetadata:\n  hermes:\n    tags: [claims, memory]\n    category: productivity\n---\n\n# Claim Review\n\nReview durable personal claims with explicit evidence.\n`,
    "claim-review:scripts/review.py": "print('Reviewing personal claims...')\n",
    "deep-research:SKILL.md": `---\nname: deep-research\ndescription: Exhaustive multi-source research investigation.\nversion: 1.0.0\nplatforms: [macos, linux]\nmetadata:\n  hermes:\n    tags: [research, web]\n    category: research\n---\n\n# Deep Research\n\nPerform in-depth multi-phase web research.\n`,
    "deep-research:templates/report.md":
      "# Research Report Template\n\n## Overview\n",
    "productivity/notion:SKILL.md": `---\nname: notion\ndescription: "Notion API + ntn CLI: pages, databases, markdown."\nversion: 2.0.0\nauthor: community\nlicense: MIT\nplatforms: [linux, macos, windows]\nprerequisites:\n  env_vars: [NOTION_API_KEY]\nmetadata:\n  hermes:\n    tags: [Notion, Productivity, Notes]\n---\n\n# Notion\n\nInteract with Notion workspaces, databases, and pages.\n`,
    "creative/claude-design:SKILL.md": `---\nname: claude-design\ndescription: Generate polished web mockups and design specs.\nversion: 1.1.0\nmetadata:\n  hermes:\n    tags: [design, ui]\n    category: creative\n---\n\n# Claude Design\n\nDraft UI components and visual guidelines.\n`,
    "apple/apple-notes:SKILL.md": `---\nname: apple-notes\ndescription: Apple Notes extraction and synchronization.\nversion: 0.9.0\nplatforms: [macos]\nmetadata:\n  hermes:\n    tags: [apple, notes]\n    category: apple\n---\n\n# Apple Notes\n\nInteract with Apple Notes.\n`,
  };

  const mockSkillRevisions: Record<string, string> = {
    "claim-review:SKILL.md": "sha256:mock-claim-md",
    "claim-review:scripts/review.py": "sha256:mock-claim-py",
    "deep-research:SKILL.md": "sha256:mock-deep-md",
    "deep-research:templates/report.md": "sha256:mock-deep-tpl",
    "productivity/notion:SKILL.md": "sha256:mock-notion-md",
    "creative/claude-design:SKILL.md": "sha256:mock-claude-md",
    "apple/apple-notes:SKILL.md": "sha256:mock-apple-md",
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

    async loadSkills() {
      requireAuthentication();
      return {
        categories: Array.from(
          new Set(mockSkills.map((s) => s.category)),
        ).sort(),
        schema: 1,
        skills: mockSkills.map((s) => ({
          author: s.author,
          bundled: s.bundled,
          category: s.category,
          description: s.description,
          enabled: s.enabled,
          fileCount: s.files.length,
          id: s.id,
          lastUsedAt: s.lastUsedAt,
          name: s.name,
          pinned: s.pinned,
          tags: s.tags,
          useCount: s.useCount,
          version: s.version,
        })),
      };
    },
    async loadSkillDetail(id) {
      requireAuthentication();
      const found = mockSkills.find((s) => s.id === id);
      if (!found) throw error(404, "skill_not_found");
      return { schema: 1, skill: { ...found } };
    },
    async loadSkillFile(id, filePath) {
      requireAuthentication();
      const key = `${id}:${filePath}`;
      const fileContent = mockSkillFiles[key];
      if (fileContent === undefined) throw error(404, "not_found");
      return {
        content: fileContent,
        editable: true,
        id,
        modified_at: modifiedAt,
        path: filePath,
        revision: mockSkillRevisions[key] ?? "sha256:mock-skill-rev",
        schema: 1,
        size: new TextEncoder().encode(fileContent).byteLength,
      };
    },
    async saveSkillFile(id, filePath, nextContent, expectedRev) {
      requireAuthentication();
      const key = `${id}:${filePath}`;
      const currentRev = mockSkillRevisions[key] ?? "sha256:mock-skill-rev";
      if (expectedRev !== currentRev) {
        throw error(409, "revision_conflict", currentRev);
      }
      mockSkillFiles[key] = nextContent;
      const nextRev = `sha256:mock-skill-${++saveCount}`;
      mockSkillRevisions[key] = nextRev;
      return {
        id,
        modified_at: new Date().toISOString(),
        ok: true,
        path: filePath,
        revision: nextRev,
        schema: 1,
        size: new TextEncoder().encode(nextContent).byteLength,
      };
    },
    async toggleSkillEnable(id, enabled) {
      requireAuthentication();
      const found = mockSkills.find((s) => s.id === id);
      if (found) found.enabled = enabled;
      return { ok: true, schema: 1 };
    },
    async toggleSkillPin(id, pinned) {
      requireAuthentication();
      const found = mockSkills.find((s) => s.id === id);
      if (found) found.pinned = pinned;
      return { ok: true, schema: 1 };
    },
    async createSkill(request) {
      requireAuthentication();
      const id = request.category
        ? `${request.category}/${request.name}`
        : request.name;
      const newSkill: (typeof mockSkills)[0] = {
        bundled: false,
        category: request.category || "standalone",
        description: request.description,
        enabled: true,
        fileCount: 1,
        files: [
          {
            editable: true,
            modified_at: new Date().toISOString(),
            path: "SKILL.md",
            size: 100,
          },
        ],
        id,
        name: request.name,
        pinned: false,
        tags: [],
        useCount: 0,
        version: "0.1.0",
      };
      mockSkills.push(newSkill);
      const key = `${id}:SKILL.md`;
      mockSkillFiles[key] =
        `---\nname: ${request.name}\ndescription: ${request.description}\nversion: 0.1.0\n---\n\n# ${request.name}\n\n${request.description}\n`;
      mockSkillRevisions[key] = "sha256:mock-created";
      return { id, ok: true, schema: 1 };
    },
  };
}
