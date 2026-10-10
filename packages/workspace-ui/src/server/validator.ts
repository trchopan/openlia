import {
  type Dirent,
  existsSync,
  readdirSync,
  readFileSync,
  statSync,
} from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import Ajv2020, { type ValidateFunction } from "ajv/dist/2020";
import addFormats from "ajv-formats";
import YAML from "yaml";
import type {
  WorkspaceDiagnosticIssue,
  WorkspaceDiagnosticsResponse,
  WorkspaceFileValidation,
} from "../shared/api";
import { isConfigurationPath, protectedPath } from "./workspace";

const compiledValidators = new Map<
  string,
  { content: string; validate: ValidateFunction }
>();

export function parseRecordFrontmatter(content: string): {
  metadata: Record<string, unknown> | null;
  body: string;
  hasFrontmatter: boolean;
} {
  if (!content.startsWith("---")) {
    return { body: content, hasFrontmatter: false, metadata: null };
  }

  const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/);
  if (!match || match[1] === undefined) {
    return { body: content, hasFrontmatter: false, metadata: null };
  }

  const rawYaml = match[1].trim();
  try {
    const parsed = YAML.parse(rawYaml);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return {
        body: match[2] ?? "",
        hasFrontmatter: true,
        metadata: parsed as Record<string, unknown>,
      };
    }
  } catch {
    return { body: match[2] ?? "", hasFrontmatter: true, metadata: null };
  }

  return { body: match[2] ?? "", hasFrontmatter: true, metadata: null };
}

export interface DomainTemplateInfo {
  templateRelativePath: string;
  schemaRelativePath: string;
  schemaAbsolutePath: string;
}

export function findDomainTemplate(
  workspaceRoot: string,
  recordRelativePath: string,
): DomainTemplateInfo | null {
  const root = resolve(workspaceRoot);
  const normalized = recordRelativePath.replaceAll("\\", "/");
  const recordAbsolute = resolve(root, normalized);
  let currentDir = dirname(recordAbsolute);

  while (currentDir === root || currentDir.startsWith(root + sep)) {
    if (existsSync(currentDir)) {
      try {
        const entries = readdirSync(currentDir, { withFileTypes: true });
        for (const entry of entries) {
          if (
            entry.isFile() &&
            entry.name.endsWith("-template.md") &&
            !entry.name.startsWith(".")
          ) {
            const templateAbsolute = join(currentDir, entry.name);
            const templateRelative = relative(
              root,
              templateAbsolute,
            ).replaceAll("\\", "/");
            const templateContent = readFileSync(templateAbsolute, "utf-8");
            const { metadata } = parseRecordFrontmatter(templateContent);
            const schemaRef =
              metadata && typeof metadata.$schema === "string"
                ? metadata.$schema.trim()
                : null;
            if (schemaRef) {
              const schemaAbsolute = resolve(currentDir, schemaRef);
              if (
                existsSync(schemaAbsolute) &&
                (schemaAbsolute === root ||
                  schemaAbsolute.startsWith(root + sep))
              ) {
                const schemaRelative = relative(
                  root,
                  schemaAbsolute,
                ).replaceAll("\\", "/");
                return {
                  schemaAbsolutePath: schemaAbsolute,
                  schemaRelativePath: schemaRelative,
                  templateRelativePath: templateRelative,
                };
              }
            }
          }
        }
      } catch {}
    }

    if (currentDir === root) break;
    currentDir = dirname(currentDir);
  }

  return null;
}

function getCompiledValidator(schemaPath: string): ValidateFunction | null {
  const absolutePath = resolve(schemaPath);
  try {
    const raw = readFileSync(absolutePath, "utf-8");
    const cached = compiledValidators.get(absolutePath);
    if (cached?.content === raw) return cached.validate;

    // Isolate schema IDs across paths and discard old registrations on edits.
    const ajv = new Ajv2020({ allErrors: true, strict: false });
    addFormats(ajv);
    const jsonSchema = JSON.parse(raw);
    const validate = ajv.compile(jsonSchema);
    compiledValidators.set(absolutePath, { content: raw, validate });
    return validate;
  } catch (_err) {
    compiledValidators.delete(absolutePath);
    return null;
  }
}

function configurationSchemaPath(
  workspaceRoot: string,
  relativePath: string,
): string | null {
  const normalized = relativePath.replaceAll("\\", "/");
  const lower = normalized.toLowerCase();
  if (lower.endsWith("/workspace-ui.yaml") || lower === "workspace-ui.yaml")
    return null;
  if (lower.endsWith(".schema.json")) return resolve(workspaceRoot, normalized);
  if (!isConfigurationPath(normalized)) return null;
  if (!lower.endsWith(".yaml") && !lower.endsWith(".yml")) return null;
  return resolve(
    workspaceRoot,
    normalized.replace(/\.(yaml|yml)$/i, ".schema.json"),
  );
}

function schemaValidation(
  workspaceRoot: string,
  data: unknown,
  schemaPath: string,
  location: string,
): WorkspaceFileValidation {
  const validator = getCompiledValidator(schemaPath);
  const schemaRelativePath = relative(
    resolve(workspaceRoot),
    schemaPath,
  ).replaceAll("\\", "/");
  if (!validator) {
    return {
      errors: [`Failed to load schema from ${schemaRelativePath}`],
      schema_path: schemaRelativePath,
      valid: false,
    };
  }
  if (validator(data)) {
    return { errors: [], schema_path: schemaRelativePath, valid: true };
  }
  const errors = (validator.errors ?? []).map((error) => {
    const path = error.instancePath
      ? `${location}${error.instancePath}`
      : location;
    return `${path}: ${error.message ?? "validation error"}`;
  });
  return {
    errors: errors.length > 0 ? errors : [`${location} does not match schema`],
    schema_path: schemaRelativePath,
    valid: false,
  };
}

function schemaDocumentValidation(
  workspaceRoot: string,
  schemaPath: string,
): WorkspaceFileValidation {
  const schemaRelativePath = relative(
    resolve(workspaceRoot),
    schemaPath,
  ).replaceAll("\\", "/");
  return getCompiledValidator(schemaPath)
    ? { errors: [], schema_path: schemaRelativePath, valid: true }
    : {
        errors: [`File is not a valid JSON Schema`],
        schema_path: schemaRelativePath,
        valid: false,
      };
}

export function validateRecordContent(
  workspaceRoot: string,
  recordRelativePath: string,
  content: string,
): WorkspaceFileValidation {
  const normalized = recordRelativePath.replaceAll("\\", "/");
  if (protectedPath(normalized)) {
    return { errors: [], valid: true };
  }

  const configurationSchema = configurationSchemaPath(
    workspaceRoot,
    normalized,
  );
  if (
    configurationSchema &&
    normalized.toLowerCase().endsWith(".schema.json")
  ) {
    try {
      JSON.parse(content);
      return schemaDocumentValidation(workspaceRoot, configurationSchema);
    } catch {
      return {
        errors: ["File is not valid JSON"],
        schema_path: normalized,
        valid: false,
      };
    }
  }
  if (configurationSchema) {
    let parsed: unknown;
    try {
      parsed = YAML.parse(content);
    } catch {
      return {
        errors: ["File is not valid YAML"],
        schema_path: relative(
          resolve(workspaceRoot),
          configurationSchema,
        ).replaceAll("\\", "/"),
        valid: false,
      };
    }
    return schemaValidation(
      workspaceRoot,
      parsed,
      configurationSchema,
      "document",
    );
  }

  if (!normalized.toLowerCase().endsWith(".md")) {
    return { errors: [], valid: true };
  }

  const domain = findDomainTemplate(workspaceRoot, normalized);
  if (!domain) {
    return { errors: [], valid: true };
  }

  const { hasFrontmatter, metadata } = parseRecordFrontmatter(content);
  if (!hasFrontmatter) {
    return {
      errors: [
        `File must begin with YAML frontmatter conforming to ${domain.templateRelativePath}`,
      ],
      schema_path: domain.schemaRelativePath,
      valid: false,
    };
  }

  if (metadata === null) {
    return {
      errors: ["YAML frontmatter is malformed or not a valid object"],
      schema_path: domain.schemaRelativePath,
      valid: false,
    };
  }

  const validator = getCompiledValidator(domain.schemaAbsolutePath);
  if (!validator) {
    return {
      errors: [`Failed to load schema from ${domain.schemaRelativePath}`],
      schema_path: domain.schemaRelativePath,
      valid: false,
    };
  }

  const dataWithoutSchema: Record<string, unknown> = {};
  for (const [key, val] of Object.entries(metadata)) {
    if (key !== "$schema") dataWithoutSchema[key] = val;
  }

  const isValid = validator(dataWithoutSchema);
  if (isValid) {
    return {
      errors: [],
      schema_path: domain.schemaRelativePath,
      valid: true,
    };
  }

  const errors: string[] = [];
  if (validator.errors) {
    for (const error of validator.errors) {
      const location = error.instancePath
        ? `frontmatter${error.instancePath}`
        : "frontmatter";
      if (
        error.keyword === "required" &&
        error.params &&
        "missingProperty" in error.params
      ) {
        errors.push(
          `${location}: missing required property '${String(error.params.missingProperty)}'`,
        );
      } else if (
        error.keyword === "additionalProperties" &&
        error.params &&
        "additionalProperty" in error.params
      ) {
        errors.push(
          `${location}: unrecognized property '${String(error.params.additionalProperty)}'`,
        );
      } else {
        errors.push(`${location}: ${error.message ?? "validation error"}`);
      }
    }
  }

  return {
    errors: errors.length > 0 ? errors : ["Frontmatter does not match schema"],
    schema_path: domain.schemaRelativePath,
    valid: false,
  };
}

export function validateEntireWorkspace(
  workspaceRoot: string,
): WorkspaceDiagnosticsResponse {
  const root = resolve(workspaceRoot);
  const issues: WorkspaceDiagnosticIssue[] = [];

  const visit = (directory: string): void => {
    let entries: Dirent[] = [];
    try {
      entries = readdirSync(directory, { withFileTypes: true });
    } catch {
      return;
    }

    for (const entry of entries) {
      const absolute = join(directory, entry.name);
      const relPath = relative(root, absolute).replaceAll("\\", "/");

      if (protectedPath(relPath)) continue;

      let info: ReturnType<typeof statSync> | undefined;
      try {
        info = statSync(absolute);
      } catch {
        continue;
      }
      if (!info || info.isSymbolicLink()) continue;

      if (entry.isDirectory()) {
        visit(absolute);
      } else if (
        entry.isFile() &&
        (relPath.toLowerCase().endsWith(".md") || isConfigurationPath(relPath))
      ) {
        try {
          const content = readFileSync(absolute, "utf-8");
          const validation = validateRecordContent(root, relPath, content);
          if (!validation.valid) {
            issues.push({
              errors: validation.errors,
              path: relPath,
              ...(validation.schema_path
                ? { schema_path: validation.schema_path }
                : {}),
            });
          }
        } catch {}
      }
    }
  };

  visit(root);
  issues.sort((a, b) => a.path.localeCompare(b.path));

  return {
    issues,
    schema: 1,
    valid: issues.length === 0,
  };
}
