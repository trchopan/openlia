import type { SkillSummary, WorkspaceTreeEntry } from "../shared/api";

export type ResolvedTarget =
  | {
      kind: "workspace";
      path: string;
      label: string;
      exists?: boolean | undefined;
    }
  | {
      kind: "skill";
      skillId: string;
      skillFile?: string | undefined;
      label: string;
      exists?: boolean | undefined;
    }
  | null;

export interface GoToSuggestion {
  kind: "workspace" | "skill";
  id: string;
  title: string;
  subtitle?: string | undefined;
  badge: string;
  skillFile?: string | undefined;
}

const COMMON_FILE_EXTENSIONS = new Set([
  ".md",
  ".markdown",
  ".txt",
  ".yaml",
  ".yml",
  ".json",
  ".toml",
  ".py",
  ".sh",
  ".bash",
  ".js",
  ".ts",
  ".csv",
]);

function hasFileExtension(name: string): boolean {
  const dotIndex = name.lastIndexOf(".");
  if (dotIndex <= 0) return false;
  return COMMON_FILE_EXTENSIONS.has(name.slice(dotIndex).toLowerCase());
}

function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

export function parseSkillRemainder(
  remainder: string,
  knownSkillIds?: string[] | undefined,
): { skillId: string; skillFile?: string | undefined } {
  const cleanRemainder = remainder.replace(/^\/+|\/+$/g, "");
  if (!cleanRemainder) {
    return { skillId: "", skillFile: undefined };
  }

  // 1. Try matching against known skill IDs if provided
  if (knownSkillIds && knownSkillIds.length > 0) {
    // Sort longer IDs first so 'productivity/notion' matches before 'productivity'
    const sortedIds = [...knownSkillIds].sort((a, b) => b.length - a.length);
    for (const id of sortedIds) {
      if (cleanRemainder === id) {
        return { skillId: id, skillFile: undefined };
      }
      if (cleanRemainder.startsWith(`${id}/`)) {
        const filePart = cleanRemainder
          .slice(id.length + 1)
          .replace(/^\/+/, "");
        return {
          skillId: id,
          skillFile: filePart || undefined,
        };
      }
    }
  }

  // 2. Heuristic parsing when skill is not in known list
  const parts = cleanRemainder.split("/").filter(Boolean);
  const p0 = parts[0] ?? "";
  const p1 = parts[1] ?? "";

  if (parts.length === 1) {
    return { skillId: p0, skillFile: undefined };
  }

  if (parts.length === 2) {
    // If parts[1] has a file extension, parts[0] is skillId and parts[1] is skillFile
    if (hasFileExtension(p1)) {
      return { skillId: p0, skillFile: p1 };
    }
    // Otherwise, treat as category/skill-name
    return { skillId: `${p0}/${p1}`, skillFile: undefined };
  }

  // parts.length >= 3
  const SKILL_SUBDIRECTORIES = new Set([
    "scripts",
    "templates",
    "references",
    "docs",
    "tests",
    "assets",
    "data",
  ]);

  if (hasFileExtension(p1) || SKILL_SUBDIRECTORIES.has(p1)) {
    return { skillId: p0, skillFile: parts.slice(1).join("/") };
  }
  return {
    skillId: `${p0}/${p1}`,
    skillFile: parts.slice(2).join("/"),
  };
}

export function resolveLinkTarget(
  input: string,
  options?: {
    skills?: SkillSummary[] | undefined;
    tree?: WorkspaceTreeEntry[] | undefined;
  },
): ResolvedTarget {
  const trimmed = input.trim();
  if (!trimmed) return null;

  const knownSkillIds = options?.skills?.map((s) => s.id) ?? [];
  const knownDocPaths = new Set(
    options?.tree?.filter((e) => e.kind === "file").map((e) => e.path) ?? [],
  );

  // 1. Handle openlia:// scheme
  if (trimmed.startsWith("openlia://")) {
    try {
      const url = new URL(trimmed);
      const host = url.host.toLowerCase();
      const rawPath = safeDecode(url.pathname.replace(/^\/+/, ""));
      const querySkillFile = url.searchParams.get("skillFile")?.trim();

      if (host === "workspace") {
        const path = rawPath.replace(/^\/+/, "");
        if (!path) return null;
        return {
          exists: knownDocPaths.size > 0 ? knownDocPaths.has(path) : undefined,
          kind: "workspace",
          label: path,
          path,
        };
      }

      if (host === "skills") {
        if (!rawPath && !querySkillFile) return null;
        const { skillId, skillFile } = parseSkillRemainder(
          rawPath,
          knownSkillIds,
        );
        const finalSkillFile = querySkillFile || skillFile;
        if (!skillId) return null;

        const skillExists =
          knownSkillIds.length > 0
            ? knownSkillIds.includes(skillId)
            : undefined;

        const label = finalSkillFile
          ? `${skillId} (${finalSkillFile})`
          : skillId;

        return {
          exists: skillExists,
          kind: "skill",
          label,
          skillFile: finalSkillFile,
          skillId,
        };
      }
    } catch {
      // Continue to fallback parsing
    }
  }

  // 2. Handle HTTP / absolute or relative web URLs
  let candidate = trimmed;
  if (/^https?:\/\//i.test(candidate)) {
    try {
      const parsedUrl = new URL(candidate);
      candidate = parsedUrl.pathname + parsedUrl.search;
    } catch {
      // ignore
    }
  }

  // /files/... or /file/...
  if (candidate.startsWith("/files/") || candidate.startsWith("/file/")) {
    const withoutPrefix = candidate.replace(/^\/files?\//, "");
    const pathPart = withoutPrefix.split("?")[0] ?? "";
    const path = safeDecode(pathPart).replace(/^\/+/, "");
    if (!path) return null;
    return {
      exists: knownDocPaths.size > 0 ? knownDocPaths.has(path) : undefined,
      kind: "workspace",
      label: path,
      path,
    };
  }

  // /skills/...
  if (candidate.startsWith("/skills/") || candidate === "/skills") {
    const withoutPrefix = candidate.replace(/^\/skills\/?/, "");
    const [pathPart = "", queryPart = ""] = withoutPrefix.split("?");
    const queryParams = new URLSearchParams(queryPart);
    const querySkillFile = queryParams.get("skillFile")?.trim();
    const rawSkillPath = safeDecode(pathPart);

    const { skillId, skillFile } = parseSkillRemainder(
      rawSkillPath,
      knownSkillIds,
    );
    const finalSkillFile = querySkillFile || skillFile;
    if (!skillId) return null;

    const skillExists =
      knownSkillIds.length > 0 ? knownSkillIds.includes(skillId) : undefined;
    const label = finalSkillFile ? `${skillId} (${finalSkillFile})` : skillId;

    return {
      exists: skillExists,
      kind: "skill",
      label,
      skillFile: finalSkillFile,
      skillId,
    };
  }

  // 3. Plain relative path fallbacks
  // If it starts with workspace/
  if (candidate.startsWith("workspace/")) {
    const path = candidate.slice("workspace/".length).replace(/^\/+/, "");
    if (!path) return null;
    return {
      exists: knownDocPaths.size > 0 ? knownDocPaths.has(path) : undefined,
      kind: "workspace",
      label: path,
      path,
    };
  }

  // If it starts with skills/
  if (candidate.startsWith("skills/")) {
    const raw = candidate.slice("skills/".length).replace(/^\/+/, "");
    const { skillId, skillFile } = parseSkillRemainder(raw, knownSkillIds);
    if (!skillId) return null;
    const skillExists =
      knownSkillIds.length > 0 ? knownSkillIds.includes(skillId) : undefined;
    const label = skillFile ? `${skillId} (${skillFile})` : skillId;
    return {
      exists: skillExists,
      kind: "skill",
      label,
      skillFile,
      skillId,
    };
  }

  // Direct match to known skill ID
  if (knownSkillIds.includes(candidate)) {
    return {
      exists: true,
      kind: "skill",
      label: candidate,
      skillId: candidate,
    };
  }

  // Direct match to known document path
  if (knownDocPaths.has(candidate)) {
    return {
      exists: true,
      kind: "workspace",
      label: candidate,
      path: candidate,
    };
  }

  // Generic fallback: if it has a file extension or looks like a file path
  if (hasFileExtension(candidate) || candidate.includes("/")) {
    const path = candidate.replace(/^\/+/, "");
    return {
      exists: knownDocPaths.size > 0 ? knownDocPaths.has(path) : undefined,
      kind: "workspace",
      label: path,
      path,
    };
  }

  // Fallback: check if matches any skill name (case-insensitive)
  const matchedSkill = options?.skills?.find(
    (s) =>
      s.name.toLowerCase() === candidate.toLowerCase() ||
      s.id.toLowerCase() === candidate.toLowerCase(),
  );
  if (matchedSkill) {
    return {
      exists: true,
      kind: "skill",
      label: matchedSkill.id,
      skillId: matchedSkill.id,
    };
  }

  // If candidate has no slash or extension and nothing matched, default to workspace document
  return {
    exists: false,
    kind: "workspace",
    label: candidate,
    path: candidate,
  };
}

export function filterGoToSuggestions(
  query: string,
  options?: {
    skills?: SkillSummary[] | undefined;
    tree?: WorkspaceTreeEntry[] | undefined;
    limit?: number | undefined;
  },
): GoToSuggestion[] {
  const q = query.trim().toLowerCase();
  const limit = options?.limit ?? 10;
  const suggestions: GoToSuggestion[] = [];

  const files = options?.tree?.filter((entry) => entry.kind === "file") ?? [];
  const skills = options?.skills ?? [];

  if (!q) {
    // Show a mix of recent/starter documents and pinned/active skills
    const pinnedSkills = skills.filter((s) => s.pinned);
    const otherSkills = skills.filter((s) => !s.pinned);
    for (const skill of [...pinnedSkills, ...otherSkills].slice(0, 5)) {
      suggestions.push({
        badge: skill.category ? `Skill / ${skill.category}` : "Skill",
        id: skill.id,
        kind: "skill",
        subtitle: skill.description,
        title: skill.name,
      });
    }

    for (const file of files.slice(0, 5)) {
      const parts = file.path.split("/");
      const domain = parts[0] && parts.length > 1 ? parts[0] : "workspace";
      suggestions.push({
        badge: domain,
        id: file.path,
        kind: "workspace",
        title: file.path,
      });
    }

    return suggestions.slice(0, limit);
  }

  // Filter skills
  for (const skill of skills) {
    const matchName = skill.name.toLowerCase().includes(q);
    const matchId = skill.id.toLowerCase().includes(q);
    const matchCategory = skill.category.toLowerCase().includes(q);
    const matchDesc = skill.description.toLowerCase().includes(q);

    if (matchName || matchId || matchCategory || matchDesc) {
      suggestions.push({
        badge: skill.category ? `Skill / ${skill.category}` : "Skill",
        id: skill.id,
        kind: "skill",
        subtitle: skill.description,
        title: skill.name,
      });
    }
    if (suggestions.length >= limit) break;
  }

  // Filter documents
  for (const file of files) {
    if (file.path.toLowerCase().includes(q)) {
      const parts = file.path.split("/");
      const domain = parts[0] && parts.length > 1 ? parts[0] : "workspace";
      suggestions.push({
        badge: domain,
        id: file.path,
        kind: "workspace",
        title: file.path,
      });
    }
    if (suggestions.length >= limit) break;
  }

  return suggestions.slice(0, limit);
}

export function buildWorkspaceLink(path: string, baseOrigin?: string): string {
  const cleanPath = path.replace(/^\/+/, "");
  const origin =
    baseOrigin !== undefined
      ? baseOrigin.replace(/\/+$/, "")
      : typeof window !== "undefined" && window.location?.origin
        ? window.location.origin
        : "";
  return `${origin}/files/${cleanPath}`;
}

export function buildSkillLink(
  skillId: string,
  skillFile?: string | undefined,
  baseOrigin?: string,
): string {
  const cleanSkillId = skillId.replace(/^\/+|\/+$/g, "");
  const origin =
    baseOrigin !== undefined
      ? baseOrigin.replace(/\/+$/, "")
      : typeof window !== "undefined" && window.location?.origin
        ? window.location.origin
        : "";
  if (!skillFile) {
    return `${origin}/skills/${cleanSkillId}`;
  }
  const cleanFile = skillFile.replace(/^\/+/, "");
  return `${origin}/skills/${cleanSkillId}/${cleanFile}`;
}

export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to textarea fallback
    }
  }
  if (typeof document !== "undefined") {
    try {
      const textArea = document.createElement("textarea");
      textArea.value = text;
      textArea.style.position = "fixed";
      textArea.style.left = "-9999px";
      textArea.style.top = "-9999px";
      textArea.style.opacity = "0";
      document.body.appendChild(textArea);
      textArea.focus();
      textArea.select();
      const success = document.execCommand("copy");
      document.body.removeChild(textArea);
      return Boolean(success);
    } catch {
      return false;
    }
  }
  return false;
}
