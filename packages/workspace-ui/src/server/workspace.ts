import { createHash, randomUUID } from "node:crypto";
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
  writeSync,
} from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import type {
  WorkspaceFile,
  WorkspaceGitStatus,
  WorkspaceTreeResponse,
  WorkspaceWriteResponse,
} from "../shared/api";

export const DEFAULT_MAX_EDITABLE_BYTES = 2 * 1024 * 1024;
export const DEFAULT_MAX_DOWNLOAD_BYTES = 100 * 1024 * 1024;
export const DEFAULT_MAX_TREE_ENTRIES = 10_000;

const editableExtensions = new Set([
  ".md",
  ".markdown",
  ".txt",
  ".yaml",
  ".yml",
  ".json",
  ".toml",
  ".csv",
]);

export interface WorkspaceServiceOptions {
  workspaceRoot: string;
  maxEditableBytes?: number;
  maxDownloadBytes?: number;
  maxTreeEntries?: number;
}

export class WorkspaceError extends Error {
  readonly code: string;
  readonly status: number;
  readonly currentRevision?: string;

  constructor(
    message: string,
    status: number,
    code: string,
    currentRevision?: string,
  ) {
    super(message);
    this.name = "WorkspaceError";
    this.code = code;
    this.status = status;
    if (currentRevision !== undefined) this.currentRevision = currentRevision;
  }
}

export function revision(contents: Uint8Array): string {
  return `sha256:${createHash("sha256").update(contents).digest("hex")}`;
}

export function protectedPath(path: string): boolean {
  const normalized = path.replaceAll("\\", "/");
  const parts = normalized.split("/");
  const basename = normalized.split("/").at(-1) ?? "";
  return (
    normalized === ".env" ||
    normalized.startsWith(".env.") ||
    normalized.endsWith(".env") ||
    normalized.includes(".secret") ||
    normalized.endsWith(".pem") ||
    normalized.endsWith(".key") ||
    normalized.endsWith(".p12") ||
    normalized.endsWith(".pfx") ||
    basename === "auth.json" ||
    normalized.startsWith("sessions/") ||
    normalized.startsWith("logs/") ||
    normalized.startsWith("cache/") ||
    normalized.startsWith("browser-profile/") ||
    normalized.startsWith("mcp-tokens/") ||
    normalized.startsWith("pairing/") ||
    normalized === ".gitignore" ||
    normalized.endsWith("/.gitignore") ||
    parts.some(
      (part) => part === ".git" || part === ".env" || part.startsWith(".env."),
    ) ||
    basename === "AGENTS.md" ||
    basename === "CLAUDE.md" ||
    basename === ".cursorrules" ||
    normalized === ".git" ||
    normalized.startsWith(".git/")
  );
}

function validateRelativePath(value: unknown): string {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    value.length > 1024 ||
    value.includes("\0")
  ) {
    throw new WorkspaceError("invalid workspace path", 400, "invalid_path");
  }
  if (value.startsWith("/") || value.includes("\\")) {
    throw new WorkspaceError("invalid workspace path", 400, "invalid_path");
  }
  const parts = value.split("/");
  if (parts.some((part) => part === "" || part === "." || part === "..")) {
    throw new WorkspaceError("invalid workspace path", 400, "invalid_path");
  }
  const normalized = parts.join("/");
  if (protectedPath(normalized)) {
    throw new WorkspaceError(
      "workspace path is protected",
      403,
      "protected_path",
    );
  }
  return normalized;
}

function fileExtension(path: string): string {
  const lastSlash = path.lastIndexOf("/");
  const lastDot = path.lastIndexOf(".");
  return lastDot > lastSlash ? path.slice(lastDot).toLowerCase() : "";
}

function asNodeError(error: unknown): { code?: string } {
  return error instanceof Error ? (error as Error & { code?: string }) : {};
}

export class WorkspaceService {
  readonly workspaceRoot: string;
  readonly maxEditableBytes: number;
  readonly maxDownloadBytes: number;
  readonly maxTreeEntries: number;

  constructor(options: WorkspaceServiceOptions) {
    this.workspaceRoot = resolve(options.workspaceRoot);
    this.maxEditableBytes =
      options.maxEditableBytes ?? DEFAULT_MAX_EDITABLE_BYTES;
    this.maxDownloadBytes =
      options.maxDownloadBytes ?? DEFAULT_MAX_DOWNLOAD_BYTES;
    this.maxTreeEntries = options.maxTreeEntries ?? DEFAULT_MAX_TREE_ENTRIES;
    mkdirSync(this.workspaceRoot, { recursive: true, mode: 0o700 });
  }

  tree(): WorkspaceTreeResponse {
    const entries: WorkspaceTreeResponse["entries"] = [];
    const visit = (directory: string): void => {
      if (entries.length >= this.maxTreeEntries) return;
      let directoryEntries: Dirent<string>[] = [];
      try {
        directoryEntries = readdirSync(directory, {
          encoding: "utf8",
          withFileTypes: true,
        });
      } catch {
        return;
      }
      for (const entry of directoryEntries) {
        const absolute = join(directory, entry.name);
        const path = relative(this.workspaceRoot, absolute)
          .split(sep)
          .join("/");
        if (protectedPath(path)) continue;
        let info: Stats | undefined;
        try {
          info = lstatSync(absolute);
        } catch {
          continue;
        }
        if (info.isSymbolicLink()) continue;
        if (entry.isDirectory()) {
          entries.push({ path, kind: "directory" });
          visit(absolute);
        } else if (entry.isFile()) {
          try {
            entries.push({
              kind: "file",
              ...this.fileMetadata(absolute, path),
            });
          } catch {
            continue;
          }
        }
        if (entries.length >= this.maxTreeEntries) return;
      }
    };

    visit(this.workspaceRoot);
    entries.sort((left, right) => left.path.localeCompare(right.path));
    return {
      schema: 1,
      entries,
      truncated: entries.length >= this.maxTreeEntries,
    };
  }

  read(pathValue: unknown): WorkspaceFile {
    const path = validateRelativePath(pathValue);
    const absolute = this.assertNoSymlink(path);
    let info: Stats | undefined;
    try {
      info = lstatSync(absolute);
    } catch (error) {
      throw this.mapFilesystemError(error, "workspace file was not found");
    }
    if (!info.isFile())
      throw new WorkspaceError(
        "workspace path is not a file",
        400,
        "not_a_file",
      );
    if (info.size > this.maxEditableBytes) {
      throw new WorkspaceError(
        "file is too large to edit",
        413,
        "file_too_large",
      );
    }
    const contents = readFileSync(absolute);
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
      schema: 1,
      content: text,
      revision: revision(contents),
      ...this.fileMetadata(absolute, path),
    };
  }

  write(
    pathValue: unknown,
    contentValue: unknown,
    expectedValue: unknown,
  ): WorkspaceWriteResponse {
    const path = validateRelativePath(pathValue);
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

    if (!editableExtensions.has(fileExtension(path))) {
      throw new WorkspaceError(
        "file type is not editable",
        415,
        "not_editable",
      );
    }

    const absolute = this.assertNoSymlink(path);
    const parent = dirname(absolute);
    mkdirSync(parent, { recursive: true, mode: 0o700 });
    let current = new Uint8Array();
    let mode = 0o600;
    if (existsSync(absolute)) {
      const info = lstatSync(absolute);
      if (!info.isFile())
        throw new WorkspaceError(
          "workspace path is not a regular file",
          400,
          "not_a_file",
        );
      if (info.size > this.maxEditableBytes) {
        throw new WorkspaceError(
          "file is too large to edit",
          413,
          "file_too_large",
        );
      }
      current = readFileSync(absolute);
      mode = info.mode & 0o777;
    }
    const currentRevision = revision(current);
    if (
      typeof expectedValue !== "string" ||
      expectedValue !== currentRevision
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
      `.${absolute.split(sep).at(-1)}.openlia-${randomUUID()}.tmp`,
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
      const beforeReplace = existsSync(absolute)
        ? readFileSync(absolute)
        : new Uint8Array();
      if (revision(beforeReplace) !== currentRevision) {
        throw new WorkspaceError(
          "revision conflict",
          409,
          "revision_conflict",
          revision(beforeReplace),
        );
      }
      renameSync(temporary, absolute);
    } catch (error) {
      if (existsSync(temporary)) unlinkSync(temporary);
      throw error;
    }
    return {
      schema: 1,
      ok: true,
      revision: revision(contents),
      ...this.fileMetadata(absolute, path),
    };
  }

  download(pathValue: unknown): {
    path: string;
    absolute: string;
    filename: string;
  } {
    const path = validateRelativePath(pathValue);
    const absolute = this.assertNoSymlink(path);
    let info: Stats | undefined;
    try {
      info = lstatSync(absolute);
    } catch (error) {
      throw this.mapFilesystemError(error, "workspace file was not found");
    }
    if (!info.isFile())
      throw new WorkspaceError(
        "workspace path is not a file",
        400,
        "not_a_file",
      );
    if (info.size > this.maxDownloadBytes) {
      throw new WorkspaceError(
        "file is too large to download",
        413,
        "download_too_large",
      );
    }
    return {
      path,
      absolute,
      filename: (path.split("/").at(-1) ?? "download").replaceAll(
        /["\r\n]/g,
        "",
      ),
    };
  }

  async gitStatus(): Promise<WorkspaceGitStatus> {
    if (!existsSync(join(this.workspaceRoot, ".git")))
      return { schema: 1, configured: false };
    const result = Bun.spawnSync(
      ["git", "-C", this.workspaceRoot, "status", "--short", "--branch"],
      {
        stderr: "pipe",
        stdout: "pipe",
      },
    );
    if (result.exitCode !== 0) return { schema: 1, configured: false };
    const status = new TextDecoder().decode(result.stdout).trim();
    const lines = status ? status.split("\n") : [];
    const branch = lines[0]?.replace(/^##\s*/, "") ?? "";
    return {
      schema: 1,
      configured: true,
      branch,
      dirty: lines.some((line) => /^\s*[MADRCU?!]/.test(line)),
      status,
    };
  }

  private fileMetadata(path: string, relativePath: string) {
    const info = statSync(path);
    return {
      path: relativePath,
      size: info.size,
      modified_at: info.mtime.toISOString(),
      editable:
        editableExtensions.has(fileExtension(relativePath)) &&
        info.size <= this.maxEditableBytes,
    };
  }

  private assertNoSymlink(relativePath: string): string {
    const absolute = resolve(this.workspaceRoot, relativePath);
    if (!this.insideWorkspace(absolute)) {
      throw new WorkspaceError(
        "workspace path is outside the workspace",
        400,
        "invalid_path",
      );
    }
    let current = this.workspaceRoot;
    for (const part of relativePath.split("/")) {
      current = join(current, part);
      if (!existsSync(current)) continue;
      const info = lstatSync(current);
      if (info.isSymbolicLink()) {
        throw new WorkspaceError(
          "workspace symlinks are not supported",
          403,
          "symlink_not_allowed",
        );
      }
    }
    return absolute;
  }

  private insideWorkspace(path: string): boolean {
    const candidate = resolve(path);
    return (
      candidate === this.workspaceRoot ||
      candidate.startsWith(this.workspaceRoot + sep)
    );
  }

  private mapFilesystemError(error: unknown, fallback: string): WorkspaceError {
    const code = asNodeError(error).code;
    if (code === "ENOENT")
      return new WorkspaceError(fallback, 404, "not_found");
    if (code === "EACCES" || code === "EPERM")
      return new WorkspaceError(
        "workspace access denied",
        403,
        "access_denied",
      );
    return new WorkspaceError(
      "workspace operation failed",
      500,
      "workspace_operation_failed",
    );
  }
}
