import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { describe, expect, test } from "vitest";
import type { WorkspaceTreeEntry } from "../shared/api";
import {
  ChatgptPreview,
  CopyLinkButton,
  DocumentPane,
  FileNavigator,
  MarkdownPreview,
  WorkspaceHeader,
} from "./components";
import { buildWorkspaceLink } from "./openliaLinks";

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

  test("renders frontmatter as a structured card in MarkdownPreview", () => {
    render(
      <MarkdownPreview
        content={`---
name: my-skill
description: A helpful skill
tags:
  - web
  - api
---
# Main Content
This is the document body.
`}
      />,
    );

    expect(screen.getByText("my-skill")).toBeInTheDocument();
    expect(screen.getByText("A helpful skill")).toBeInTheDocument();
    expect(screen.getByText("web")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Main Content" }),
    ).toBeInTheDocument();
    expect(screen.getByText("This is the document body.")).toBeInTheDocument();
  });

  test("renders a ChatGPT export as a conversation with sources", () => {
    render(
      <ChatgptPreview
        content={`schema: 1
session:
  platform: chatgpt
  topic: Preview topic
  model: ChatGPT
messages:
  - role: user
    content: What happened?
  - role: assistant
    content: |
      # The answer

      It is documented.
references:
  - id: ref-1
    title: Documentation
    url: https://openlia.example/docs
`}
      />,
    );

    expect(
      screen.getByRole("article", { name: "ChatGPT conversation" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Preview topic" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "The answer" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Sources (1)" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Documentation/ })).toHaveAttribute(
      "href",
      "https://openlia.example/docs",
    );
  });

  test("falls back to raw YAML when a ChatGPT export is invalid", () => {
    render(<ChatgptPreview content="schema: [invalid" />);

    expect(
      screen.getByRole("article", { name: "Raw chat export" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/could not be parsed/)).toBeInTheDocument();
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

describe("CopyLinkButton and document copy integration", () => {
  test("renders copy button and updates label to Copied! on click", async () => {
    let copiedText = "";
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          copiedText = text;
        },
      },
    });

    const expectedLink = buildWorkspaceLink("tasks.md");
    render(<CopyLinkButton link={expectedLink} />);
    const button = screen.getByRole("button", {
      name: /copy link/i,
    });
    expect(button).toBeInTheDocument();

    fireEvent.click(button);
    await waitFor(() => {
      expect(copiedText).toBe(expectedLink);
      expect(screen.getByText("Copied!")).toBeInTheDocument();
    });
  });

  test("WorkspaceHeader renders copy link button when currentLink is provided", async () => {
    let copied = "";
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          copied = text;
        },
      },
    });

    const expectedLink = buildWorkspaceLink("tasks.md");
    render(
      <WorkspaceHeader
        authRequired={false}
        currentLink={expectedLink}
        dirty={false}
        file={{
          content: "hello",
          editable: true,
          modified_at: "2026-09-22T00:00:00.000Z",
          path: "tasks.md",
          revision: "rev1",
          schema: 1,
          size: 5,
        }}
        filesButtonRef={{ current: null }}
        git={null}
        onOpenFiles={() => undefined}
        onSignOut={() => undefined}
      />,
    );

    const copyBtn = screen.getByRole("button", {
      name: `Copy link: ${expectedLink}`,
    });
    expect(copyBtn).toBeInTheDocument();
    fireEvent.click(copyBtn);
    expect(copied).toBe(expectedLink);
  });

  test("DocumentPane renders copy link button in toolbar", async () => {
    let copied = "";
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          copied = text;
        },
      },
    });

    const expectedLink = buildWorkspaceLink("tasks.md");
    render(
      <DocumentPane
        conflict=""
        diff={[]}
        documentError=""
        draft="hello"
        file={{
          content: "hello",
          editable: true,
          modified_at: "2026-09-22T00:00:00.000Z",
          path: "tasks.md",
          revision: "rev1",
          schema: 1,
          size: 5,
        }}
        fileLoading={false}
        onCloseFiles={() => undefined}
        onDownload={() => undefined}
        onDraftChange={() => undefined}
        onOpenDetails={() => undefined}
        onRetry={() => undefined}
        onSave={() => undefined}
        onViewChange={() => undefined}
        saving={false}
        view="preview"
      />,
    );

    const copyBtn = screen.getByRole("button", {
      name: `Copy link (${expectedLink})`,
    });
    expect(copyBtn).toBeInTheDocument();
    fireEvent.click(copyBtn);
    expect(copied).toBe(expectedLink);
  });

  test("DocumentPane renders Reveal in Tree button and fires callback", () => {
    let revealed = false;
    render(
      <DocumentPane
        conflict=""
        diff={[]}
        documentError=""
        draft="hello"
        file={{
          content: "hello",
          editable: true,
          modified_at: "2026-09-22T00:00:00.000Z",
          path: "tasks.md",
          revision: "rev1",
          schema: 1,
          size: 5,
        }}
        fileLoading={false}
        onCloseFiles={() => undefined}
        onDownload={() => undefined}
        onDraftChange={() => undefined}
        onOpenDetails={() => undefined}
        onRevealInTree={() => {
          revealed = true;
        }}
        onRetry={() => undefined}
        onSave={() => undefined}
        onViewChange={() => undefined}
        saving={false}
        view="preview"
      />,
    );

    const revealBtn = screen.getByRole("button", {
      name: "Reveal in tree",
    });
    expect(revealBtn).toBeInTheDocument();
    fireEvent.click(revealBtn);
    expect(revealed).toBe(true);
  });
});
