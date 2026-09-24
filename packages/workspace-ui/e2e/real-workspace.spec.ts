import { expect, test, type Page } from "@playwright/test";
import { captureScreenshot } from "./screenshot";

const realWorkspace = process.env.WORKSPACE_UI_E2E_MODE === "real";

async function openFiles(page: Page) {
  await expect(page.getByRole("heading", { name: "Workspace" })).toBeVisible();
  const filesButton = page.getByRole("button", { exact: true, name: "Files" });
  if (await filesButton.isVisible()) {
    await filesButton.click();
    await expect(
      page
        .locator("aside.fixed")
        .getByRole("heading", { exact: true, name: "Files" })
        .first(),
    ).toBeVisible();
  }
}

test.describe("copied workspace visual verification", () => {
  test.skip(
    !realWorkspace,
    "Real workspace suite requires WORKSPACE_UI_E2E_MODE=real",
  );

  test("shows the copied workspace", async ({ page }, testInfo) => {
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Workspace" }),
    ).toBeVisible();
    await openFiles(page);
    await expect(
      page.getByRole("button", { name: "README.md", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: ".gitkeep", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "event-note-template.md", exact: true }),
    ).toHaveCount(0);
    await expect(page.getByLabel("archive, empty folder")).toBeVisible();
    await captureScreenshot(page, testInfo, "copied-workspace");
  });

  test("filters and opens a copied workspace document", async ({
    page,
  }, testInfo) => {
    await page.goto("/");
    await openFiles(page);
    await page.getByLabel("Find files").fill("calendar");
    const event = page.getByRole("button", {
      name: "2026-09-23-lunch-marukame-fe-dev-2.md",
      exact: true,
    });
    await expect(event).toBeVisible();
    await event.click();
    await expect(
      page.getByRole("textbox", { name: "Document editor" }),
    ).not.toHaveValue("");
    await captureScreenshot(page, testInfo, "copied-calendar-document");
  });

  test("does not expose starter templates through search", async ({ page }) => {
    await page.goto("/");
    await openFiles(page);
    await page.getByLabel("Find files").fill("template");
    await expect(page.getByText(/No files match/)).toBeVisible();
  });

  test("renders a ChatGPT export as a read-only conversation", async ({
    page,
  }, testInfo) => {
    await page.goto("/");
    await openFiles(page);
    await page.getByLabel("Find files").fill("vietnam_crime");
    const chat = page.getByRole("button", {
      name: /20260924_092233_vietnam_crime_rate_comparison/i,
    });
    await expect(chat).toBeVisible();
    await chat.click();

    await expect(
      page.getByRole("article", { name: "ChatGPT conversation" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", {
        name: "Vietnam crime rate comparison Asia EU United States",
      }),
    ).toBeVisible();
    await expect(page.getByText("Read-only chat export")).toBeVisible();
    await expect(
      page.getByRole("textbox", { name: "Document editor" }),
    ).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Save" })).toHaveCount(0);
    await captureScreenshot(page, testInfo, "chatgpt-conversation");
  });
});
