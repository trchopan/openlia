import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { App } from "./App";
import type { WorkspaceApi } from "./api";
import { createMockWorkspaceApi } from "./mockApi";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("workspace application", () => {
  test("opens a document, tracks edits, and saves it", async () => {
    render(<App api={createMockWorkspaceApi()} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
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

  test("protects a dirty draft before switching documents", async () => {
    render(<App api={createMockWorkspaceApi()} />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
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

  test("automatically loads document from route path on initial load/reload", async () => {
    window.history.replaceState(null, "", "/files/notes.md");
    render(<App api={createMockWorkspaceApi()} />);

    const editor = await screen.findByRole("textbox", {
      name: "Document editor",
    });
    expect(editor).toHaveValue("Hello");
    expect(document.title).toBe("notes.md - OpenLia Workspace");
  });

  test("restores view and filter query params from URL on reload", async () => {
    window.history.replaceState(
      null,
      "",
      "/files/notes.md?view=preview&filter=calendar",
    );
    render(<App api={createMockWorkspaceApi()} />);

    expect(
      await screen.findByRole("article", { name: "Markdown preview" }),
    ).toBeInTheDocument();
    const filterInput = screen.getByLabelText("Find files");
    expect(filterInput).toHaveValue("calendar");
  });

  test("updates URL when user opens a document, switches views, and filters", async () => {
    window.history.replaceState(null, "", "/");
    render(<App api={createMockWorkspaceApi()} />);

    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    await screen.findByRole("textbox", { name: "Document editor" });
    expect(window.location.pathname).toBe("/files/notes.md");

    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(window.location.search).toContain("view=preview");

    const filterInput = screen.getByLabelText("Find files");
    fireEvent.change(filterInput, { target: { value: "task" } });
    expect(window.location.search).toContain("filter=task");
  });

  test("navigates on popstate event (browser history back)", async () => {
    window.history.replaceState(null, "", "/files/notes.md");
    render(<App api={createMockWorkspaceApi()} />);

    await screen.findByRole("textbox", { name: "Document editor" });

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
});
