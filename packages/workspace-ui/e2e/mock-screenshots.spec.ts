import { expect, test } from "@playwright/test";
import { captureScreenshot } from "./screenshot";

const realWorkspace = process.env.WORKSPACE_UI_E2E_MODE === "real";

test.describe("mock visual catalog", () => {
  test.skip(realWorkspace, "Mock catalog is not used in real-workspace mode");

  test("workspace overview", async ({ page }, testInfo) => {
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Document workbench" }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "notes.md" })).toBeVisible();
    await captureScreenshot(page, testInfo, "workspace-overview");
  });

  test("selected document", async ({ page }, testInfo) => {
    await page.goto("/");
    await page.getByRole("button", { name: "notes.md" }).click();
    await expect(
      page.getByRole("textbox", { name: "Document editor" }),
    ).toHaveValue("Hello");
    await captureScreenshot(page, testInfo, "selected-document");
  });

  test("edited draft and revision context", async ({ page }, testInfo) => {
    await page.goto("/");
    await page.getByRole("button", { name: "notes.md" }).click();
    const editor = page.getByRole("textbox", { name: "Document editor" });
    await editor.fill("Hello\nThis is a screenshot verification draft.");
    await expect(page.getByRole("button", { name: "Save" })).toBeEnabled();
    await captureScreenshot(page, testInfo, "edited-draft");
  });

  test("filtered tree", async ({ page }, testInfo) => {
    await page.goto("/");
    await page.getByLabel("Find files").fill("calendar");
    await expect(
      page.getByRole("button", { name: "calendar/event.md" }),
    ).toBeVisible();
    await captureScreenshot(page, testInfo, "filtered-tree");
  });

  test("empty search result", async ({ page }, testInfo) => {
    await page.goto("/");
    await page.getByLabel("Find files").fill("does-not-exist");
    await expect(page.getByText("No matching files.")).toBeVisible();
    await captureScreenshot(page, testInfo, "empty-search-result");
  });

  test("authentication gate", async ({ page }, testInfo) => {
    await page.goto("/?scenario=auth");
    await expect(page.getByLabel("Password")).toBeVisible();
    await captureScreenshot(page, testInfo, "authentication-gate");
  });

  test("authenticated workspace", async ({ page }, testInfo) => {
    await page.goto("/?scenario=auth");
    await page.getByLabel("Password").fill("local-playwright-password");
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByRole("button", { name: "notes.md" })).toBeVisible();
    await captureScreenshot(page, testInfo, "authenticated-workspace");
  });

  test("revision conflict", async ({ page }, testInfo) => {
    await page.goto("/?scenario=conflict");
    await page.getByRole("button", { name: "notes.md" }).click();
    await page
      .getByRole("textbox", { name: "Document editor" })
      .fill("A conflicting draft");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("alert")).toContainText("Revision conflict");
    await captureScreenshot(page, testInfo, "revision-conflict");
  });

  test("loading state", async ({ page }, testInfo) => {
    await page.goto("/?scenario=loading");
    await expect(
      page.getByText("Loading workspace...", { exact: true }),
    ).toBeVisible();
    await captureScreenshot(page, testInfo, "loading");
  });

  test("empty workspace", async ({ page }, testInfo) => {
    await page.goto("/?scenario=empty");
    await expect(page.getByText("No matching files.")).toBeVisible();
    await captureScreenshot(page, testInfo, "empty-workspace");
  });

  test("load error", async ({ page }, testInfo) => {
    await page.goto("/?scenario=error");
    await expect(page.getByRole("alert")).toContainText(
      "Workspace data could not be loaded.",
    );
    await captureScreenshot(page, testInfo, "load-error");
  });
});
