import { describe, expect, test } from "bun:test";

describe("browser-tools package", () => {
  test("reserves a Node-compatible package boundary", async () => {
    const packageData = await Bun.file(
      new URL("../package.json", import.meta.url),
    ).json();
    expect(packageData.type).toBe("module");
    expect(packageData.name).toBe("@openlia/browser-tools");
  });
});
