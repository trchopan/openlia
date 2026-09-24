import { expect, test } from "bun:test";

test("the browser job client passes its self-test", () => {
  const result = Bun.spawnSync([
    "bun",
    "profile/skills/browser-pilot/scripts/openlia_job.ts",
    "--self-test",
  ]);
  expect(result.exitCode).toBe(0);
  expect(new TextDecoder().decode(result.stdout).trim()).toBe("ok");
});
