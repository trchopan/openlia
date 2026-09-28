import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { App } from "./App";
import type { WorkspaceApi } from "./api";
import { createMockWorkspaceApi } from "./mockApi";
import { buildWorkspaceLink } from "./openliaLinks";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("workspace application", () => {
  test("opens a document, tracks edits, and saves it", async () => {
    render(<App api={createMockWorkspaceApi()} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const editor = await screen.findByRole("textbox", {
      name: "Document editor",
    });
    expect(editor).toHaveValue("Hello");
    fireEvent.change(editor, { target: { value: "Hello\nworld" } });
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Saved" })).toBeDisabled(),
    );
    expect(screen.getByRole("button", { name: "Saved" })).toBeDisabled();
  });

  test("keeps the draft visible when saving encounters a revision conflict", async () => {
    render(<App api={createMockWorkspaceApi({ scenario: "conflict" })} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const editor = await screen.findByRole("textbox", {
      name: "Document editor",
    });
    fireEvent.change(editor, { target: { value: "draft" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This file changed after you opened it.",
    );
    expect(editor).toHaveValue("draft");
  });

  test("confirms and deletes the selected document", async () => {
    render(<App api={createMockWorkspaceApi()} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));

    fireEvent.click(screen.getByRole("button", { name: "Delete file" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent(
      'permanently delete "notes.md"',
    );
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Delete",
      }),
    );

    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Choose a document to begin" }),
      ).toBeInTheDocument(),
    );
    await waitFor(() => expect(window.location.pathname).toBe("/"));
    expect(screen.queryByRole("button", { name: "notes.md" })).toBeNull();
    expect(window.location.pathname).toBe("/");
  });

  test("requires explicit discard before deleting a dirty document", async () => {
    render(<App api={createMockWorkspaceApi()} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const editor = await screen.findByRole("textbox", {
      name: "Document editor",
    });
    fireEvent.change(editor, { target: { value: "unsaved" } });

    fireEvent.click(screen.getByRole("button", { name: "Delete file" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent(
      "Deleting it will discard the draft",
    );
    fireEvent.click(screen.getByRole("button", { name: "Discard and delete" }));

    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Choose a document to begin" }),
      ).toBeInTheDocument(),
    );
    expect(screen.queryByDisplayValue("unsaved")).toBeNull();
  });

  test("protects a dirty draft before switching documents", async () => {
    render(<App api={createMockWorkspaceApi()} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const editor = await screen.findByRole("textbox", {
      name: "Document editor",
    });
    fireEvent.change(editor, { target: { value: "draft" } });
    fireEvent.click(screen.getByRole("button", { name: "calendar" }));
    fireEvent.click(screen.getByRole("button", { name: "event.md" }));

    expect(await screen.findByRole("dialog")).toHaveTextContent(
      "Keep your draft?",
    );
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(editor).toHaveValue("draft");
  });

  test("shows the login gate and loads the workspace after authentication", async () => {
    render(<App api={createMockWorkspaceApi({ scenario: "auth" })} />);
    const password = await screen.findByLabelText("Password");
    fireEvent.change(password, {
      target: { value: "correct horse battery staple" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(
      await screen.findByRole("button", { name: "notes.md" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).toBeNull();
  });

  test("keeps workspace files visible when Git status is unavailable", async () => {
    const baseApi = createMockWorkspaceApi();
    const api: WorkspaceApi = {
      ...baseApi,
      async loadGitStatus() {
        throw new Error("git_unavailable");
      },
    };

    render(<App api={api} />);

    expect(
      await screen.findByRole("button", { name: "notes.md" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Version control unavailable")).toBeInTheDocument();
  });

  test("automatically loads document from route path on initial load/reload", async () => {
    window.history.replaceState(null, "", "/files/notes.md");
    render(<App api={createMockWorkspaceApi()} />);

    expect(
      await screen.findByRole("article", { name: "Markdown preview" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Hello")).toBeInTheDocument();
    expect(document.title).toBe("notes.md - OpenLia Workspace");
  });

  test("restores view and filter query params from URL on reload", async () => {
    window.history.replaceState(
      null,
      "",
      "/files/notes.md?view=edit&filter=calendar",
    );
    render(<App api={createMockWorkspaceApi()} />);

    expect(
      await screen.findByRole("textbox", { name: "Document editor" }),
    ).toBeInTheDocument();
    const filterInput = screen.getByLabelText("Find files");
    expect(filterInput).toHaveValue("calendar");
  });

  test("updates URL when user opens a document, switches views, and filters", async () => {
    window.history.replaceState(null, "", "/");
    render(<App api={createMockWorkspaceApi()} />);

    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    await screen.findByRole("article", { name: "Markdown preview" });
    expect(window.location.pathname).toBe("/files/notes.md");

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(window.location.search).toContain("view=edit");

    const filterInput = screen.getByLabelText("Find files");
    fireEvent.change(filterInput, { target: { value: "task" } });
    expect(window.location.search).toContain("filter=task");
  });

  test("navigates on popstate event (browser history back)", async () => {
    window.history.replaceState(null, "", "/files/notes.md");
    render(<App api={createMockWorkspaceApi()} />);

    await screen.findByRole("article", { name: "Markdown preview" });

    window.history.replaceState(null, "", "/");
    fireEvent(window, new PopStateEvent("popstate"));

    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Choose a document to begin" }),
      ).toBeInTheDocument(),
    );
  });

  test("displays error and retry button when route document cannot be opened", async () => {
    window.history.replaceState(null, "", "/files/missing.md");
    render(<App api={createMockWorkspaceApi()} />);

    expect(
      await screen.findByText("This document could not be opened. Try again."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  test("opens ChatGPT exports in a read-only conversation preview", async () => {
    const path = "knowledge/chatgpt/export.yaml";
    const content = `schema: 1
session:
  platform: chatgpt
  topic: A saved conversation
messages:
  - role: user
    content: What is this?
  - role: assistant
    content: It is a saved conversation.
`;
    const baseApi = createMockWorkspaceApi();
    const chatApi: WorkspaceApi = {
      ...baseApi,
      async loadFile(requestedPath) {
        if (requestedPath === path)
          return {
            content,
            editable: false,
            modified_at: "2026-09-24T00:00:00.000Z",
            path,
            revision: "sha256:chat",
            schema: 1,
            size: new TextEncoder().encode(content).byteLength,
          };
        return baseApi.loadFile(requestedPath);
      },
      async loadTree() {
        const response = await baseApi.loadTree();
        return {
          ...response,
          entries: [
            ...response.entries,
            { kind: "directory", path: "knowledge" },
            { kind: "directory", path: "knowledge/chatgpt" },
            {
              editable: false,
              kind: "file",
              modified_at: "2026-09-24T00:00:00.000Z",
              path,
              size: new TextEncoder().encode(content).byteLength,
            },
          ],
        };
      },
    };

    window.history.replaceState(null, "", `/files/${path}`);
    render(<App api={chatApi} />);

    expect(
      await screen.findByRole("article", { name: "ChatGPT conversation" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "A saved conversation" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Read-only chat export")).toBeInTheDocument();
    expect(
      screen.queryByRole("textbox", { name: "Document editor" }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  test("opens Go To modal via header button and navigates to document", async () => {
    render(<App api={createMockWorkspaceApi()} />);

    // Click Go To button in header
    const goToButton = await screen.findByRole("button", {
      name: /Go to document or skill/i,
    });
    fireEvent.click(goToButton);

    const input = await screen.findByPlaceholderText(
      "Paste link or path, or type file/skill name...",
    );
    expect(input).toBeInTheDocument();

    // Paste web link
    fireEvent.change(input, {
      target: {
        value: "https://workspace.example.com/files/calendar/event.md",
      },
    });

    expect(screen.getByText("calendar/event.md")).toBeInTheDocument();

    // Submit
    fireEvent.keyDown(input, { key: "Enter" });

    // The document should now be open
    expect(
      await screen.findByRole("heading", { name: "Calendar event" }),
    ).toBeInTheDocument();
  });

  test("opens Go To modal via Cmd+P shortcut", async () => {
    render(<App api={createMockWorkspaceApi()} />);

    // Press Cmd+P
    fireEvent.keyDown(window, { key: "p", metaKey: true });

    expect(
      await screen.findByPlaceholderText(
        "Paste link or path, or type file/skill name...",
      ),
    ).toBeInTheDocument();
  });

  test("protects dirty draft when navigating via Go To modal", async () => {
    render(<App api={createMockWorkspaceApi()} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const editor = await screen.findByRole("textbox", {
      name: "Document editor",
    });
    fireEvent.change(editor, { target: { value: "unsaved changes" } });

    // Open Go To modal and paste link
    fireEvent.keyDown(window, { key: "p", metaKey: true });
    const input = await screen.findByPlaceholderText(
      "Paste link or path, or type file/skill name...",
    );
    fireEvent.change(input, {
      target: { value: "/files/calendar/event.md" },
    });
    fireEvent.keyDown(input, { key: "Enter" });

    // Dirty draft dialog should appear
    expect(await screen.findByRole("dialog")).toHaveTextContent(
      "Keep your draft?",
    );
  });

  test("navigates in-app when clicking link in markdown preview", async () => {
    window.history.replaceState(null, "", "/");
    const baseApi = createMockWorkspaceApi();
    const customApi: WorkspaceApi = {
      ...baseApi,
      async loadFile(requestedPath) {
        if (requestedPath === "notes.md") {
          const original = await baseApi.loadFile(requestedPath);
          return {
            ...original,
            content: "[Check Calendar](/files/calendar/event.md)",
          };
        }
        return baseApi.loadFile(requestedPath);
      },
    };

    render(<App api={customApi} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Preview" }));

    // Click the markdown link in preview
    const link = await screen.findByRole("link", { name: "Check Calendar" });
    expect(link).toHaveAttribute("href", "/files/calendar/event.md");
    fireEvent.click(link);

    // Should navigate to calendar/event.md without opening a new tab
    expect(
      await screen.findByRole("heading", { name: "Calendar event" }),
    ).toBeInTheDocument();
  });

  test("allows copying document link to clipboard from document view", async () => {
    window.history.replaceState(null, "", "/");
    let copiedText = "";
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          copiedText = text;
        },
      },
    });

    render(<App api={createMockWorkspaceApi()} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));

    const expectedLink = buildWorkspaceLink("notes.md");
    // Document toolbar should have the Copy Link button
    const copyButton = await screen.findByRole("button", {
      name: `Copy link (${expectedLink})`,
    });
    expect(copyButton).toBeInTheDocument();

    fireEvent.click(copyButton);
    expect(copiedText).toBe(expectedLink);
  });

  test("clicking Reveal in Tree clears filter and focuses the file", async () => {
    window.history.replaceState(null, "", "/");
    render(<App api={createMockWorkspaceApi()} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));

    // Put a filter that excludes notes.md
    const filterInput = screen.getByPlaceholderText("Search paths");
    fireEvent.change(filterInput, { target: { value: "calendar" } });
    expect(filterInput).toHaveValue("calendar");

    // Click Reveal in Tree
    const revealBtn = await screen.findByRole("button", {
      name: "Reveal in tree",
    });
    fireEvent.click(revealBtn);

    // Filter should be cleared
    expect(filterInput).toHaveValue("");
    expect(
      await screen.findByRole("button", { name: "notes.md" }),
    ).toBeInTheDocument();
  });

  test("live updates file content when window regains focus and document is not dirty", async () => {
    window.history.replaceState(null, "", "/");
    let currentContent = "Original notes";
    let currentRevision = "rev-1";

    const baseApi = createMockWorkspaceApi();
    const customApi: WorkspaceApi = {
      ...baseApi,
      async loadFile(requestedPath) {
        if (requestedPath === "notes.md") {
          return {
            content: currentContent,
            editable: true,
            modified_at: new Date().toISOString(),
            path: "notes.md",
            revision: currentRevision,
            schema: 1,
            size: currentContent.length,
          };
        }
        return baseApi.loadFile(requestedPath);
      },
    };

    render(<App api={customApi} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));

    expect(await screen.findByText("Original notes")).toBeInTheDocument();

    // External agent updates the file on disk
    currentContent = "Updated notes from agent";
    currentRevision = "rev-2";

    // Simulate window focus / tab revalidation
    fireEvent(window, new Event("focus"));

    // Content should update automatically without user refresh
    await waitFor(() => {
      expect(screen.getByText("Updated notes from agent")).toBeInTheDocument();
    });
  });

  test("flags conflict on live update when user has unsaved draft changes", async () => {
    window.history.replaceState(null, "", "/");
    let currentContent = "Original notes";
    let currentRevision = "rev-1";

    const baseApi = createMockWorkspaceApi();
    const customApi: WorkspaceApi = {
      ...baseApi,
      async loadFile(requestedPath) {
        if (requestedPath === "notes.md") {
          return {
            content: currentContent,
            editable: true,
            modified_at: new Date().toISOString(),
            path: "notes.md",
            revision: currentRevision,
            schema: 1,
            size: currentContent.length,
          };
        }
        return baseApi.loadFile(requestedPath);
      },
    };

    render(<App api={customApi} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));

    const editor = await screen.findByRole("textbox", {
      name: "Document editor",
    });
    fireEvent.change(editor, { target: { value: "My unsaved user edits" } });

    // External agent updates the file on disk
    currentContent = "Agent edits from background";
    currentRevision = "rev-2";

    // Simulate window focus
    fireEvent(window, new Event("focus"));

    // User draft must NOT be overwritten, and conflict alert should be displayed
    await waitFor(() => {
      expect(screen.getByRole("alert")).toHaveTextContent(
        "The file changed on disk. Your unsaved draft is preserved; review before saving.",
      );
    });
    expect(editor).toHaveValue("My unsaved user edits");
  });
});
