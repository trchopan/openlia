import { mkdirSync } from "node:fs";
import { dirname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import type { Page, TestInfo } from "@playwright/test";

const packageRoot = fileURLToPath(new URL("..", import.meta.url));
const screenshotRoot = resolve(packageRoot, ".playwright/screenshots");

export async function captureScreenshot(
  page: Page,
  testInfo: TestInfo,
  name: string,
): Promise<string> {
  const outputPath = resolve(
    screenshotRoot,
    testInfo.project.name,
    `${name}.png`,
  );
  if (!outputPath.startsWith(`${screenshotRoot}${sep}`))
    throw new Error("Screenshot path escaped the local artifact directory");
  mkdirSync(dirname(outputPath), { recursive: true });
  await page.screenshot({ fullPage: true, path: outputPath });
  return outputPath;
}
