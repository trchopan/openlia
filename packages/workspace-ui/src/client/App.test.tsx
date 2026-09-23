import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { App } from "./App";
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
});
