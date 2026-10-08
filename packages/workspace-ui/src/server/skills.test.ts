import { afterEach, beforeEach, describe, expect, it } from "bun:test";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createWorkspaceHandler } from "./app";
import { SkillService, parseSkillFrontmatter } from "./skills";

describe("SkillService", () => {
  let tempRoot: string;
  let skillsRoot: string;
  let workspaceRoot: string;
  let service: SkillService;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "openlia-skills-test-"));
    skillsRoot = join(tempRoot, "skills");
    workspaceRoot = join(tempRoot, "workspace");
    mkdirSync(skillsRoot, { recursive: true });
    mkdirSync(workspaceRoot, { recursive: true });

    // Seed a standalone skill
    const standaloneDir = join(skillsRoot, "sample-skill");
    mkdirSync(join(standaloneDir, "scripts"), { recursive: true });
    writeFileSync(
      join(standaloneDir, "SKILL.md"),
      `---
name: sample-skill
description: A sample standalone skill for service tests.
version: 0.1.0
platforms: [macos, linux]
prerequisites:
  env_vars: [SAMPLE_API_KEY]
metadata:
  hermes:
    tags: [sample, testing]
    category: productivity
---

# Sample Skill
Instructions for testing the skill service.
`,
      "utf-8",
    );
    writeFileSync(
      join(standaloneDir, "scripts", "run.py"),
      "print('reviewing')",
      "utf-8",
    );

    // Seed a category skill: productivity/notion
    const categoryDir = join(skillsRoot, "productivity", "notion");
    mkdirSync(categoryDir, { recursive: true });
    writeFileSync(
      join(categoryDir, "SKILL.md"),
      `---
name: notion
description: Notion API + ntn CLI
version: 2.0.0
author: community
license: MIT
prerequisites:
  env_vars: [NOTION_API_KEY]
metadata:
  hermes:
    tags: [notes, database]
---

# Notion
Notion instructions.
`,
      "utf-8",
    );

    // Seed .usage.json
    writeFileSync(
      join(skillsRoot, ".usage.json"),
      JSON.stringify({
        "sample-skill": {
          last_used_at: "2026-09-24T00:00:00.000Z",
          pinned: true,
          state: "active",
          use_count: 5,
        },
      }),
      "utf-8",
    );

    // Seed .bundled_manifest
    writeFileSync(
      join(skillsRoot, ".bundled_manifest"),
      "sample-skill:abcdef123456\n",
      "utf-8",
    );

    service = new SkillService({ skillsRoot });
  });

  afterEach(() => {
    rmSync(tempRoot, { force: true, recursive: true });
  });

  it("parses YAML frontmatter correctly", () => {
    const raw = `---
name: test-skill
description: A test description
version: 1.2.3
---
# Content`;
    const { frontmatter, body } = parseSkillFrontmatter(raw);
    expect(frontmatter.name).toBe("test-skill");
    expect(frontmatter.description).toBe("A test description");
    expect(frontmatter.version).toBe("1.2.3");
    expect(body.trim()).toBe("# Content");
  });

  it("lists all skills and categories with metadata and usage stats", () => {
    const response = service.list();
    expect(response.schema).toBe(1);
    expect(response.skills.length).toBe(2);

    const sample = response.skills.find((s) => s.id === "sample-skill");
    expect(sample).toBeDefined();
    expect(sample?.name).toBe("sample-skill");
    expect(sample?.pinned).toBe(true);
    expect(sample?.bundled).toBe(true);
    expect(sample?.useCount).toBe(5);
    expect(sample?.enabled).toBe(true);
    expect(sample?.tags).toEqual(["sample", "testing"]);

    const notion = response.skills.find((s) => s.id === "productivity/notion");
    expect(notion).toBeDefined();
    expect(notion?.name).toBe("notion");
    expect(notion?.category).toBe("productivity");
    expect(notion?.pinned).toBe(false);

    expect(response.categories).toContain("productivity");
  });

  it("returns full skill detail including files and prerequisites", () => {
    const detail = service.detail("sample-skill");
    expect(detail.schema).toBe(1);
    expect(detail.skill.name).toBe("sample-skill");
    expect(detail.skill.prerequisites?.env_vars).toEqual(["SAMPLE_API_KEY"]);
    expect(detail.skill.platforms).toEqual(["macos", "linux"]);
    expect(detail.skill.files.some((f) => f.path === "SKILL.md")).toBe(true);
    expect(detail.skill.files.some((f) => f.path === "scripts/run.py")).toBe(
      true,
    );
  });

  it("reads and writes skill files atomically with revision checks", () => {
    const file = service.readFile("sample-skill", "SKILL.md");
    expect(file.schema).toBe(1);
    expect(file.content).toContain("# Sample Skill");
    expect(file.editable).toBe(true);
    expect(file.revision).toBeTruthy();

    const newContent = `${file.content}\n## New Section\n`;

    // Conflict on stale revision
    expect(() =>
      service.writeFile("sample-skill", "SKILL.md", newContent, "sha256:stale"),
    ).toThrow();

    // Success with matching revision
    const writeResult = service.writeFile(
      "sample-skill",
      "SKILL.md",
      newContent,
      file.revision,
    );
    expect(writeResult.ok).toBe(true);
    expect(writeResult.revision).not.toBe(file.revision);

    const updated = service.readFile("sample-skill", "SKILL.md");
    expect(updated.content).toBe(newContent);
    expect(updated.revision).toBe(writeResult.revision);
  });

  it("rejects path traversal and unsafe identifiers", () => {
    expect(() => service.detail("../outside")).toThrow();
    expect(() => service.readFile("sample-skill", "../secret.txt")).toThrow();
    expect(() => service.readFile("sample-skill", "/etc/passwd")).toThrow();
  });

  it("toggles enabled / disabled state", () => {
    service.toggleEnable("sample-skill", false);

    // Should now be inside .openlia-disabled/sample-skill
    expect(
      existsSync(join(skillsRoot, ".openlia-disabled", "sample-skill")),
    ).toBe(true);
    expect(existsSync(join(skillsRoot, "sample-skill"))).toBe(false);

    let list = service.list();
    let sample = list.skills.find((s) => s.id === "sample-skill");
    expect(sample?.enabled).toBe(false);

    // Toggle back to enabled
    service.toggleEnable("sample-skill", true);
    expect(existsSync(join(skillsRoot, "sample-skill"))).toBe(true);
    expect(
      existsSync(join(skillsRoot, ".openlia-disabled", "sample-skill")),
    ).toBe(false);

    list = service.list();
    sample = list.skills.find((s) => s.id === "sample-skill");
    expect(sample?.enabled).toBe(true);
  });

  it("toggles pin status in .usage.json", () => {
    service.togglePin("productivity/notion", true);
    let list = service.list();
    let notion = list.skills.find((s) => s.id === "productivity/notion");
    expect(notion?.pinned).toBe(true);

    service.togglePin("productivity/notion", false);
    list = service.list();
    notion = list.skills.find((s) => s.id === "productivity/notion");
    expect(notion?.pinned).toBe(false);
  });

  it("creates a new skill with template", () => {
    const id = service.create(
      "custom-helper",
      "tools",
      "A custom helper skill",
    );
    expect(id).toBe("tools/custom-helper");

    const createdSkillPath = join(
      skillsRoot,
      "tools",
      "custom-helper",
      "SKILL.md",
    );
    expect(existsSync(createdSkillPath)).toBe(true);

    const content = readFileSync(createdSkillPath, "utf-8");
    expect(content).toContain("name: custom-helper");
    expect(content).toContain("A custom helper skill");

    const detail = service.detail("tools/custom-helper");
    expect(detail.skill.name).toBe("custom-helper");
    expect(detail.skill.category).toBe("tools");
  });

  it("integrates with HTTP handler", async () => {
    const handler = createWorkspaceHandler({
      authRequired: false,
      skillsRoot,
      workspaceRoot,
    });

    // GET /api/skills/list
    const listRes = await handler(
      new Request("http://127.0.0.1:8089/api/skills/list"),
    );
    expect(listRes.status).toBe(200);
    const listData = (await listRes.json()) as { skills: unknown[] };
    expect(listData.skills.length).toBe(2);

    // GET /api/skills/detail
    const detailRes = await handler(
      new Request("http://127.0.0.1:8089/api/skills/detail?id=sample-skill"),
    );
    expect(detailRes.status).toBe(200);
    const detailData = (await detailRes.json()) as { skill: { name: string } };
    expect(detailData.skill.name).toBe("sample-skill");

    // POST /api/skills/create
    const createRes = await handler(
      new Request("http://127.0.0.1:8089/api/skills/create", {
        body: JSON.stringify({
          category: "test-cat",
          description: "Testing API create",
          name: "api-created",
        }),
        headers: { "Content-Type": "application/json" },
        method: "POST",
      }),
    );
    expect(createRes.status).toBe(201);
    const createData = (await createRes.json()) as {
      ok: boolean;
      id: string;
    };
    expect(createData.ok).toBe(true);
    expect(createData.id).toBe("test-cat/api-created");
  });
});
