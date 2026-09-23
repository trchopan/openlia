import { expect, test } from "bun:test";

test("the distributed job client is built from the runtime skill", () => {
  const result = Bun.spawnSync([
    "bun",
    "profile/skills/browser-pilot/scripts/openlia_job.ts",
    "--self-test",
  ]);
  expect(result.exitCode).toBe(0);
  expect(new TextDecoder().decode(result.stdout).trim()).toBe("ok");
});
