import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import type { WorkspaceTreeEntry } from "../shared/api";
import { FileNavigator, MarkdownPreview } from "./components";

describe("workspace presentation components", () => {
  test("renders safe GFM structures semantically", () => {
    const { container } = render(
      <MarkdownPreview
        content={
          "# Notes\n\n- [x] shipped\n\n| Area | State |\n| --- | --- |\n| UI | ready |\n\n<script>alert(1)</script>"
        }
      />,
    );

    expect(screen.getByRole("heading", { name: "Notes" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox")).toBeDisabled();
    expect(screen.getByRole("table")).toBeInTheDocument();
    expect(container.querySelector("script")).toBeNull();
  });

  test("sorts folders and files naturally while keeping empty folders visible", () => {
    const entries: WorkspaceTreeEntry[] = [
      { kind: "directory", path: "zeta" },
      { kind: "directory", path: "alpha" },
      {
        editable: true,
        kind: "file",
        modified_at: "2026-09-22T00:00:00.000Z",
        path: "alpha/10.md",
        size: 1,
      },
      {
        editable: true,
        kind: "file",
        modified_at: "2026-09-22T00:00:00.000Z",
        path: "alpha/2.md",
        size: 1,
      },
      {
        editable: true,
        kind: "file",
        modified_at: "2026-09-22T00:00:00.000Z",
        path: "root.md",
        size: 1,
      },
    ];

    render(
      <FileNavigator
        entries={entries}
        filter=""
        loading={false}
        mobileOpen={false}
        onClose={() => undefined}
        onFilterChange={() => undefined}
        onOpenFile={() => undefined}
        onRetry={() => undefined}
        selectedPath={undefined}
        truncated={false}
        workspaceError=""
      />,
    );

    const navigator = screen.getByLabelText("Workspace files");
    const treeButtons = within(navigator)
      .getAllByRole("button")
      .map((button) => button.textContent?.replace(/[▾▸]/, ""));
    expect(treeButtons).toEqual(["Close", "alpha", "root.md"]);
    expect(screen.getByLabelText("zeta, empty folder")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "alpha" }));
    const alphaFiles = within(navigator)
      .getAllByRole("button")
      .map((button) => button.textContent);
    expect(alphaFiles).toContain("2.md");
    expect(alphaFiles).toContain("10.md");
    expect(alphaFiles.indexOf("2.md")).toBeLessThan(
      alphaFiles.indexOf("10.md"),
    );
  });

  test("forces matching folders open and disables collapse during search", () => {
    render(
      <FileNavigator
        entries={[
          { kind: "directory", path: "projects" },
          {
            editable: true,
            kind: "file",
            modified_at: "2026-09-22T00:00:00.000Z",
            path: "projects/roadmap.md",
            size: 1,
          },
        ]}
        filter="roadmap"
        loading={false}
        mobileOpen={false}
        onClose={() => undefined}
        onFilterChange={() => undefined}
        onOpenFile={() => undefined}
        onRetry={() => undefined}
        selectedPath={undefined}
        truncated={false}
        workspaceError=""
      />,
    );

    expect(screen.getByRole("button", { name: "projects" })).toBeDisabled();
  });
});
