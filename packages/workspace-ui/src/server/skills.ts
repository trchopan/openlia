import { randomUUID } from "node:crypto";
import type { Dirent, Stats } from "node:fs";
import {
  closeSync,
  existsSync,
  fsyncSync,
  lstatSync,
  mkdirSync,
  openSync,
  readdirSync,
  readFileSync,
  renameSync,
  statSync,
  unlinkSync,
  writeFileSync,
  writeSync,
} from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import YAML from "yaml";
import type {
  SkillDetail,
  SkillDetailResponse,
  SkillFileEntry,
  SkillFileResponse,
  SkillPrerequisites,
  SkillsListResponse,
  SkillSummary,
  SkillWriteResponse,
} from "../shared/api";
import {
  DEFAULT_MAX_EDITABLE_BYTES,
  protectedPath,
  revision,
  WorkspaceError,
} from "./workspace";

const editableExtensions = new Set([
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

export interface SkillServiceOptions {
  skillsRoot: string;
  maxEditableBytes?: number | undefined;
}

interface ParsedFrontmatter {
  name?: string;
  description?: string;
  version?: string;
  author?: string;
  license?: string;
  platforms?: string[];
  prerequisites?: SkillPrerequisites;
  metadata?: {
    hermes?: {
      category?: string;
      tags?: string[];
    };
    category?: string;
    tags?: string[];
  };
}

interface UsageRecord {
  state?: string;
  use_count?: number;
  view_count?: number;
  pinned?: boolean;
  last_used_at?: string;
  created_at?: string;
}

type UsageMap = Record<string, UsageRecord>;

export function parseSkillFrontmatter(content: string): {
  frontmatter: ParsedFrontmatter;
  body: string;
} {
  const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/);
  if (!match || match[1] === undefined || match[2] === undefined) {
    return { body: content, frontmatter: {} };
  }
  try {
    const parsed = YAML.parse(match[1]);
    return {
      body: match[2],
      frontmatter: typeof parsed === "object" && parsed !== null ? parsed : {},
    };
  } catch {
    return { body: content, frontmatter: {} };
  }
}

export function validateSkillId(id: unknown): string {
  if (typeof id !== "string" || !id.trim()) {
    throw new WorkspaceError(
      "invalid skill identifier",
      400,
      "invalid_skill_id",
    );
  }
  const normalized = id.replaceAll("\\", "/").trim();
  const parts = normalized.split("/");
  if (parts.length > 2 || parts.some((p) => !/^[a-zA-Z0-9_-]+$/.test(p))) {
    throw new WorkspaceError(
      "invalid skill identifier format",
      400,
      "invalid_skill_id",
    );
  }
  return normalized;
}

export function validateSkillFilePath(pathValue: unknown): string {
  if (
    typeof pathValue !== "string" ||
    pathValue.length === 0 ||
    pathValue.length > 1024 ||
    pathValue.includes("\0")
  ) {
    throw new WorkspaceError("invalid skill file path", 400, "invalid_path");
  }
  if (pathValue.startsWith("/") || pathValue.includes("\\")) {
    throw new WorkspaceError("invalid skill file path", 400, "invalid_path");
  }
  const parts = pathValue.split("/");
  if (parts.some((part) => part === "" || part === "." || part === "..")) {
    throw new WorkspaceError("invalid skill file path", 400, "invalid_path");
  }
  const normalized = parts.join("/");
  if (protectedPath(normalized)) {
    throw new WorkspaceError(
      "skill file path is protected",
      403,
      "protected_path",
    );
  }
  return normalized;
}

export class SkillService {
  readonly skillsRoot: string;
  readonly maxEditableBytes: number;

  constructor(options: SkillServiceOptions) {
    this.skillsRoot = resolve(options.skillsRoot);
    this.maxEditableBytes =
      options.maxEditableBytes ?? DEFAULT_MAX_EDITABLE_BYTES;
    mkdirSync(this.skillsRoot, { recursive: true, mode: 0o700 });
  }

  private disabledRoot(): string {
    return join(this.skillsRoot, ".openlia-disabled");
  }

  private loadUsage(): UsageMap {
    const usagePath = join(this.skillsRoot, ".usage.json");
    if (!existsSync(usagePath)) return {};
    try {
      const data = readFileSync(usagePath, "utf-8");
      const parsed = JSON.parse(data);
      return typeof parsed === "object" && parsed !== null ? parsed : {};
    } catch {
      return {};
    }
  }

  private saveUsage(usage: UsageMap): void {
    const usagePath = join(this.skillsRoot, ".usage.json");
    writeFileSync(usagePath, JSON.stringify(usage, null, 2), "utf-8");
  }

  private loadBundledManifest(): Set<string> {
    const manifestPath = join(this.skillsRoot, ".bundled_manifest");
    const set = new Set<string>();
    if (!existsSync(manifestPath)) return set;
    try {
      const lines = readFileSync(manifestPath, "utf-8").split("\n");
      for (const line of lines) {
        const [name] = line.trim().split(":");
        if (name) set.add(name.trim());
      }
    } catch {}
    return set;
  }

  private resolveSkillPath(id: string): { absolute: string; enabled: boolean } {
    const activePath = join(this.skillsRoot, ...id.split("/"));
    if (existsSync(activePath)) {
      return { absolute: activePath, enabled: true };
    }
    const disabledPath = join(this.disabledRoot(), ...id.split("/"));
    if (existsSync(disabledPath)) {
      return { absolute: disabledPath, enabled: false };
    }
    throw new WorkspaceError("skill not found", 404, "skill_not_found");
  }

  private fileExtension(path: string): string {
    const lastSlash = path.lastIndexOf("/");
    const lastDot = path.lastIndexOf(".");
    return lastDot > lastSlash ? path.slice(lastDot).toLowerCase() : "";
  }

  private listFiles(dir: string, baseDir = dir): SkillFileEntry[] {
    const results: SkillFileEntry[] = [];
    if (!existsSync(dir)) return results;
    let dirents: Dirent<string>[] = [];
    try {
      dirents = readdirSync(dir, { encoding: "utf8", withFileTypes: true });
    } catch {
      return results;
    }
    for (const entry of dirents) {
      const absolute = join(dir, entry.name);
      const rel = relative(baseDir, absolute).replaceAll("\\", "/");
      if (protectedPath(rel)) continue;
      let info: Stats;
      try {
        info = lstatSync(absolute);
      } catch {
        continue;
      }
      if (info.isSymbolicLink()) continue;
      if (entry.isDirectory()) {
        results.push(...this.listFiles(absolute, baseDir));
      } else if (entry.isFile()) {
        results.push({
          editable:
            editableExtensions.has(this.fileExtension(rel)) &&
            info.size <= this.maxEditableBytes,
          modified_at: info.mtime.toISOString(),
          path: rel,
          size: info.size,
        });
      }
    }
    return results;
  }

  list(): SkillsListResponse {
    const usage = this.loadUsage();
    const bundled = this.loadBundledManifest();
    const skills: SkillSummary[] = [];
    const categoriesSet = new Set<string>();

    const scanDirectory = (root: string, enabled: boolean) => {
      if (!existsSync(root)) return;
      let dirents: Dirent<string>[] = [];
      try {
        dirents = readdirSync(root, { encoding: "utf8", withFileTypes: true });
      } catch {
        return;
      }

      for (const entry of dirents) {
        if (!entry.isDirectory()) continue;
        if (entry.name.startsWith(".")) continue;

        const entryPath = join(root, entry.name);
        const skillMdPath = join(entryPath, "SKILL.md");

        if (existsSync(skillMdPath)) {
          // Standalone skill at top level
          const skill = this.buildSummary(
            entry.name,
            entryPath,
            enabled,
            usage,
            bundled,
          );
          skills.push(skill);
          if (skill.category) categoriesSet.add(skill.category);
        } else {
          // Check if it's a category folder containing skills
          const categoryName = entry.name;
          let subEntries: Dirent<string>[] = [];
          try {
            subEntries = readdirSync(entryPath, {
              encoding: "utf8",
              withFileTypes: true,
            });
          } catch {
            continue;
          }

          let categoryHasSkills = false;
          for (const sub of subEntries) {
            if (!sub.isDirectory() || sub.name.startsWith(".")) continue;
            const subSkillPath = join(entryPath, sub.name);
            if (existsSync(join(subSkillPath, "SKILL.md"))) {
              const skillId = `${categoryName}/${sub.name}`;
              const skill = this.buildSummary(
                skillId,
                subSkillPath,
                enabled,
                usage,
                bundled,
                categoryName,
              );
              skills.push(skill);
              categoryHasSkills = true;
            }
          }
          if (categoryHasSkills) {
            categoriesSet.add(categoryName);
          }
        }
      }
    };

    scanDirectory(this.skillsRoot, true);
    scanDirectory(this.disabledRoot(), false);

    skills.sort((a, b) => {
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
      return a.name.localeCompare(b.name);
    });

    const categories = Array.from(categoriesSet).sort();

    return {
      categories,
      schema: 1,
      skills,
    };
  }

  private buildSummary(
    id: string,
    dir: string,
    enabled: boolean,
    usage: UsageMap,
    bundled: Set<string>,
    fallbackCategory = "standalone",
  ): SkillSummary {
    const skillMdPath = join(dir, "SKILL.md");
    let name: string = id.includes("/") ? (id.split("/")[1] ?? id) : id;
    let description = "";
    let version: string | undefined;
    let author: string | undefined;
    let category: string = fallbackCategory;
    let tags: string[] = [];

    if (existsSync(skillMdPath)) {
      try {
        const content = readFileSync(skillMdPath, "utf-8");
        const { frontmatter } = parseSkillFrontmatter(content);
        if (frontmatter.name) name = frontmatter.name;
        if (frontmatter.description) description = frontmatter.description;
        if (frontmatter.version) version = String(frontmatter.version);
        if (frontmatter.author) author = frontmatter.author;
        const frontCategory =
          frontmatter.metadata?.hermes?.category ??
          frontmatter.metadata?.category;
        if (frontCategory) category = frontCategory;
        const frontTags =
          frontmatter.metadata?.hermes?.tags ?? frontmatter.metadata?.tags;
        if (Array.isArray(frontTags)) tags = frontTags.map(String);
      } catch {}
    }

    const usageEntry = usage[name] ?? usage[id] ?? {};
    const files = this.listFiles(dir);

    return {
      author,
      bundled: bundled.has(name) || bundled.has(id),
      category,
      description,
      enabled,
      fileCount: files.length,
      id,
      lastUsedAt: usageEntry.last_used_at,
      name,
      pinned: Boolean(usageEntry.pinned),
      tags,
      useCount: usageEntry.use_count ?? 0,
      version,
    };
  }

  detail(idValue: unknown): SkillDetailResponse {
    const id = validateSkillId(idValue);
    const { absolute, enabled } = this.resolveSkillPath(id);
    const usage = this.loadUsage();
    const bundled = this.loadBundledManifest();
    const skillMdPath = join(absolute, "SKILL.md");

    let name: string = id.includes("/") ? (id.split("/")[1] ?? id) : id;
    let description = "";
    let version: string | undefined;
    let author: string | undefined;
    let license: string | undefined;
    let platforms: string[] | undefined;
    let prerequisites: SkillPrerequisites | undefined;
    let category: string = id.includes("/")
      ? (id.split("/")[0] ?? "standalone")
      : "standalone";
    let tags: string[] = [];

    if (existsSync(skillMdPath)) {
      try {
        const content = readFileSync(skillMdPath, "utf-8");
        const { frontmatter } = parseSkillFrontmatter(content);
        if (frontmatter.name) name = frontmatter.name;
        if (frontmatter.description) description = frontmatter.description;
        if (frontmatter.version) version = String(frontmatter.version);
        if (frontmatter.author) author = frontmatter.author;
        if (frontmatter.license) license = frontmatter.license;
        if (Array.isArray(frontmatter.platforms))
          platforms = frontmatter.platforms.map(String);
        if (frontmatter.prerequisites)
          prerequisites = frontmatter.prerequisites;
        const frontCategory =
          frontmatter.metadata?.hermes?.category ??
          frontmatter.metadata?.category;
        if (frontCategory) category = frontCategory;
        const frontTags =
          frontmatter.metadata?.hermes?.tags ?? frontmatter.metadata?.tags;
        if (Array.isArray(frontTags)) tags = frontTags.map(String);
      } catch {}
    }

    const usageEntry = usage[name] ?? usage[id] ?? {};
    const files = this.listFiles(absolute);

    const detail: SkillDetail = {
      author,
      bundled: bundled.has(name) || bundled.has(id),
      category,
      description,
      enabled,
      fileCount: files.length,
      files,
      id,
      lastUsedAt: usageEntry.last_used_at,
      license,
      name,
      pinned: Boolean(usageEntry.pinned),
      platforms,
      prerequisites,
      tags,
      useCount: usageEntry.use_count ?? 0,
      version,
    };

    return {
      schema: 1,
      skill: detail,
    };
  }

  readFile(idValue: unknown, pathValue: unknown): SkillFileResponse {
    const id = validateSkillId(idValue);
    const relPath = validateSkillFilePath(pathValue);
    const { absolute } = this.resolveSkillPath(id);
    const targetFile = resolve(absolute, relPath);

    if (!targetFile.startsWith(absolute + sep) && targetFile !== absolute) {
      throw new WorkspaceError(
        "path is outside skill directory",
        403,
        "invalid_path",
      );
    }

    if (!existsSync(targetFile)) {
      throw new WorkspaceError("skill file not found", 404, "not_found");
    }

    const info = lstatSync(targetFile);
    if (info.isSymbolicLink()) {
      throw new WorkspaceError(
        "symlinks not supported",
        403,
        "symlink_not_allowed",
      );
    }
    if (!info.isFile()) {
      throw new WorkspaceError("target is not a file", 400, "not_a_file");
    }
    if (info.size > this.maxEditableBytes) {
      throw new WorkspaceError("file too large to edit", 413, "file_too_large");
    }

    const contents = readFileSync(targetFile);
    if (contents.includes(0)) {
      throw new WorkspaceError(
        "binary files are not editable",
        415,
        "binary_file",
      );
    }

    let text: string;
    try {
      text = new TextDecoder("utf-8", { fatal: true }).decode(contents);
    } catch {
      throw new WorkspaceError("file is not valid UTF-8", 415, "invalid_utf8");
    }

    return {
      content: text,
      editable: editableExtensions.has(this.fileExtension(relPath)),
      id,
      modified_at: info.mtime.toISOString(),
      path: relPath,
      revision: revision(contents),
      schema: 1,
      size: info.size,
    };
  }

  writeFile(
    idValue: unknown,
    pathValue: unknown,
    contentValue: unknown,
    expectedRevisionValue: unknown,
  ): SkillWriteResponse {
    const id = validateSkillId(idValue);
    const relPath = validateSkillFilePath(pathValue);
    if (typeof contentValue !== "string") {
      throw new WorkspaceError(
        "content must be a string",
        400,
        "invalid_content",
      );
    }
    const contents = new TextEncoder().encode(contentValue);
    if (contents.byteLength > this.maxEditableBytes) {
      throw new WorkspaceError(
        "file is too large to edit",
        413,
        "file_too_large",
      );
    }
    if (contents.includes(0)) {
      throw new WorkspaceError(
        "binary content is not editable",
        415,
        "binary_content",
      );
    }

    const { absolute } = this.resolveSkillPath(id);
    const targetFile = resolve(absolute, relPath);

    if (!targetFile.startsWith(absolute + sep)) {
      throw new WorkspaceError(
        "path is outside skill directory",
        403,
        "invalid_path",
      );
    }

    const parent = dirname(targetFile);
    mkdirSync(parent, { recursive: true, mode: 0o700 });

    let current = new Uint8Array();
    let mode = 0o644;
    if (existsSync(targetFile)) {
      const info = lstatSync(targetFile);
      if (info.isSymbolicLink()) {
        throw new WorkspaceError(
          "symlinks not supported",
          403,
          "symlink_not_allowed",
        );
      }
      if (!info.isFile()) {
        throw new WorkspaceError("target is not a file", 400, "not_a_file");
      }
      current = readFileSync(targetFile);
      mode = info.mode & 0o777;
    }

    const currentRevision = revision(current);
    if (
      typeof expectedRevisionValue !== "string" ||
      expectedRevisionValue !== currentRevision
    ) {
      throw new WorkspaceError(
        "revision conflict",
        409,
        "revision_conflict",
        currentRevision,
      );
    }

    const temporary = join(
      parent,
      `.${targetFile.split(sep).at(-1)}.openlia-${randomUUID()}.tmp`,
    );
    const descriptor = openSync(temporary, "wx", mode);
    try {
      writeSync(descriptor, contents);
      fsyncSync(descriptor);
    } catch (error) {
      if (existsSync(temporary)) unlinkSync(temporary);
      throw error;
    } finally {
      closeSync(descriptor);
    }

    try {
      renameSync(temporary, targetFile);
    } catch (error) {
      if (existsSync(temporary)) unlinkSync(temporary);
      throw error;
    }

    const info = statSync(targetFile);
    return {
      id,
      modified_at: info.mtime.toISOString(),
      ok: true,
      path: relPath,
      revision: revision(contents),
      schema: 1,
      size: info.size,
    };
  }

  toggleEnable(idValue: unknown, enabled: boolean): void {
    const id = validateSkillId(idValue);
    const { absolute, enabled: currentEnabled } = this.resolveSkillPath(id);
    if (currentEnabled === enabled) return;

    const parts = id.split("/");
    const destination = enabled
      ? join(this.skillsRoot, ...parts)
      : join(this.disabledRoot(), ...parts);

    mkdirSync(dirname(destination), { recursive: true, mode: 0o700 });
    renameSync(absolute, destination);

    // Clean up empty disabled parent folder if needed
    if (!enabled) {
      // moved to disabled
    } else {
      // moved from disabled, check if disabled parent category folder is now empty
      if (parts.length > 1 && parts[0]) {
        const disabledCategory = join(this.disabledRoot(), parts[0]);
        try {
          if (
            existsSync(disabledCategory) &&
            readdirSync(disabledCategory).length === 0
          ) {
            unlinkSync(disabledCategory);
          }
        } catch {}
      }
    }
  }

  togglePin(idValue: unknown, pinned: boolean): void {
    const id = validateSkillId(idValue);
    const { absolute } = this.resolveSkillPath(id);
    const skillMdPath = join(absolute, "SKILL.md");
    let name: string = id.includes("/") ? (id.split("/")[1] ?? id) : id;

    if (existsSync(skillMdPath)) {
      try {
        const content = readFileSync(skillMdPath, "utf-8");
        const { frontmatter } = parseSkillFrontmatter(content);
        if (frontmatter.name) name = frontmatter.name;
      } catch {}
    }

    const usage = this.loadUsage();
    const entry = usage[name];
    if (!entry) {
      usage[name] = {
        created_at: new Date().toISOString(),
        pinned,
        state: "active",
        use_count: 0,
        view_count: 0,
      };
    } else {
      entry.pinned = pinned;
    }
    this.saveUsage(usage);
  }

  create(
    nameValue: unknown,
    categoryValue?: unknown,
    descriptionValue?: unknown,
  ): string {
    if (
      typeof nameValue !== "string" ||
      !/^[a-z0-9]+(-[a-z0-9]+)*$/.test(nameValue.trim())
    ) {
      throw new WorkspaceError(
        "invalid skill name: must be kebab-case (e.g. my-skill)",
        400,
        "invalid_name",
      );
    }
    const name = nameValue.trim();
    let category = "";
    if (typeof categoryValue === "string" && categoryValue.trim()) {
      category = categoryValue.trim().toLowerCase();
      if (!/^[a-z0-9_-]+$/.test(category)) {
        throw new WorkspaceError(
          "invalid category: alphanumeric characters only",
          400,
          "invalid_category",
        );
      }
    }
    const description =
      typeof descriptionValue === "string" ? descriptionValue.trim() : "";

    const id = category ? `${category}/${name}` : name;
    const targetDir = category
      ? join(this.skillsRoot, category, name)
      : join(this.skillsRoot, name);

    if (
      existsSync(targetDir) ||
      existsSync(join(this.disabledRoot(), ...id.split("/")))
    ) {
      throw new WorkspaceError(
        "skill already exists",
        409,
        "skill_already_exists",
      );
    }

    mkdirSync(targetDir, { recursive: true, mode: 0o700 });

    const frontmatterObj: Record<string, unknown> = {
      description: description || `Custom skill for ${name}`,
      name,
      version: "0.1.0",
    };
    if (category) {
      frontmatterObj.metadata = { hermes: { category } };
    }

    const starterContent = `---\n${YAML.stringify(frontmatterObj)}---\n\n# ${name}\n\n${description || "Describe what this skill does and how to use it."}\n`;

    writeFileSync(join(targetDir, "SKILL.md"), starterContent, "utf-8");
    return id;
  }
}
