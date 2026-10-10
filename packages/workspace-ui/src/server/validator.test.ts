import { afterEach, beforeEach, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { validateRecordContent } from "./validator";

let root = "";

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "openlia-validator-"));
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
});

function writeDomain(directory: string, status: string) {
  mkdirSync(join(root, directory), { recursive: true });
  writeFileSync(
    join(root, directory, "record-template.md"),
    "---\n$schema: ./record-template.schema.json\nstatus: posted\n---\n",
  );
  writeFileSync(
    join(root, directory, "record-template.schema.json"),
    JSON.stringify({
      $schema: "https://json-schema.org/draft/2020-12/schema",
      $id: "urn:openlia:test:record",
      type: "object",
      required: ["status"],
      properties: { status: { const: status } },
    }),
  );
}

const content = "---\nstatus: posted\n---\n# Record\n";

test("reuses a schema for repeated reads and multiple records", () => {
  writeDomain("finance/intake", "posted");
  for (const path of ["first.md", "first.md", "second.md", "first.md"]) {
    expect(
      validateRecordContent(root, `finance/intake/${path}`, content),
    ).toEqual({
      errors: [],
      schema_path: "finance/intake/record-template.schema.json",
      valid: true,
    });
  }
});

test("recompiles edited schemas and recovers after an invalid edit", () => {
  writeDomain("finance", "posted");
  const validate = () =>
    validateRecordContent(root, "finance/record.md", content);
  expect(validate().valid).toBe(true);

  writeDomain("finance", "pending");
  expect(validate().errors).toEqual([
    "frontmatter/status: must be equal to constant",
  ]);
  expect(validate().valid).toBe(false);

  writeFileSync(join(root, "finance/record-template.schema.json"), "{");
  expect(validate().errors).toEqual([
    "Failed to load schema from finance/record-template.schema.json",
  ]);

  writeDomain("finance", "posted");
  expect(validate().valid).toBe(true);
});

test("isolates schemas with the same ID at different paths", () => {
  writeDomain("finance", "posted");
  writeDomain("reviews", "pending");
  expect(validateRecordContent(root, "finance/record.md", content).valid).toBe(
    true,
  );
  expect(
    validateRecordContent(root, "reviews/record.md", content).errors,
  ).toEqual(["frontmatter/status: must be equal to constant"]);
  expect(validateRecordContent(root, "finance/record.md", content).valid).toBe(
    true,
  );
});
