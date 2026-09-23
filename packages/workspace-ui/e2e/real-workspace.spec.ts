import { expect, test } from "@playwright/test";
import { captureScreenshot } from "./screenshot";

const realWorkspace = process.env.WORKSPACE_UI_E2E_MODE === "real";

test.describe("copied workspace visual verification", () => {
  test.skip(
    !realWorkspace,
    "Real workspace suite requires WORKSPACE_UI_E2E_MODE=real",
  );

  test("shows the copied workspace", async ({ page }, testInfo) => {
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Document workbench" }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "README.md" })).toBeVisible();
    await captureScreenshot(page, testInfo, "copied-workspace");
  });

  test("filters and opens a copied workspace document", async ({
    page,
  }, testInfo) => {
    await page.goto("/");
    await page.getByLabel("Find files").fill("calendar");
    const event = page.getByRole("button", {
      name: "calendar/2026-09-23-lunch-marukame-fe-dev-2.md",
    });
    await expect(event).toBeVisible();
    await event.click();
    await expect(
      page.getByRole("textbox", { name: "Document editor" }),
    ).not.toHaveValue("");
    await captureScreenshot(page, testInfo, "copied-calendar-document");
  });
});
