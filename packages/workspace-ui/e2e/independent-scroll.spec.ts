/// <reference lib="dom" />

import { expect, test } from "@playwright/test";

test.describe("workspace pane scrolling", () => {
  test("keeps the file tree and document content independently scrollable", async ({
    page,
  }, testInfo) => {
    test.skip(
      testInfo.project.name !== "desktop",
      "Independent pane sizing is covered at the desktop breakpoint",
    );

    await page.setViewportSize({ height: 400, width: 1440 });
    await page.goto("/files/calendar/reminders.rem?view=edit");
    await expect(
      page.getByRole("textbox", { name: "Code editor" }),
    ).toBeVisible();

    for (const directory of ["finance", "projects", "tasks"]) {
      await page.getByRole("button", { exact: true, name: directory }).click();
    }

    const metrics = await page.evaluate(() => {
      const layout = document.querySelector<HTMLElement>(".workspace-layout");
      const navigator = document.querySelector<HTMLElement>(
        ".workspace-navigator > div.overflow-auto",
      );
      const editor = document.querySelector<HTMLElement>(
        ".workspace-codemirror .cm-scroller",
      );

      if (!layout || !navigator || !editor) {
        throw new Error("Workspace scroll containers were not found");
      }

      return {
        bodyClientHeight: document.documentElement.clientHeight,
        bodyScrollHeight: document.documentElement.scrollHeight,
        editorClientHeight: editor.clientHeight,
        editorScrollHeight: editor.scrollHeight,
        layoutClientHeight: layout.clientHeight,
        layoutScrollHeight: layout.scrollHeight,
        navigatorClientHeight: navigator.clientHeight,
        navigatorScrollHeight: navigator.scrollHeight,
      };
    });

    expect(metrics.bodyScrollHeight).toBeLessThanOrEqual(
      metrics.bodyClientHeight + 1,
    );
    expect(metrics.layoutScrollHeight).toBeLessThanOrEqual(
      metrics.layoutClientHeight + 1,
    );
    expect(metrics.navigatorScrollHeight).toBeGreaterThan(
      metrics.navigatorClientHeight,
    );
    expect(metrics.editorScrollHeight).toBeGreaterThan(
      metrics.editorClientHeight,
    );

    const scrollPositions = await page.evaluate(() => {
      const navigator = document.querySelector<HTMLElement>(
        ".workspace-navigator > div.overflow-auto",
      );
      const editor = document.querySelector<HTMLElement>(
        ".workspace-codemirror .cm-scroller",
      );

      if (!navigator || !editor) {
        throw new Error("Workspace scroll containers were not found");
      }

      navigator.scrollTop = 120;
      const navigatorTop = navigator.scrollTop;
      editor.scrollTop = 120;

      return {
        editorTop: editor.scrollTop,
        navigatorTop,
        navigatorTopAfterEditorScroll: navigator.scrollTop,
      };
    });

    expect(scrollPositions.navigatorTop).toBeGreaterThan(0);
    expect(scrollPositions.editorTop).toBeGreaterThan(0);
    expect(scrollPositions.navigatorTopAfterEditorScroll).toBe(
      scrollPositions.navigatorTop,
    );
  });
});
