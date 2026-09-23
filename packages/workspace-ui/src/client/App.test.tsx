import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { App } from "./App";

const treeResponse = {
  entries: [
    {
      editable: true,
      kind: "file" as const,
      modified_at: "2026-09-22T00:00:00.000Z",
      path: "notes.md",
      size: 5,
    },
  ],
  schema: 1 as const,
  truncated: false,
};

const fileResponse = {
  ...treeResponse.entries[0],
  content: "Hello",
  revision: "sha256:before",
  schema: 1 as const,
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("workspace application", () => {
  test("opens a document, tracks edits, and saves it", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (init?.method === "PUT") {
          return new Response(
            JSON.stringify({
              ...treeResponse.entries[0],
              ok: true,
              revision: "sha256:after",
              schema: 1,
            }),
            { status: 200 },
          );
        }
        if (url.includes("/api/workspace/tree"))
          return new Response(JSON.stringify(treeResponse));
        if (url.includes("/api/workspace/git/status"))
          return new Response(JSON.stringify({ configured: false, schema: 1 }));
        return new Response(JSON.stringify(fileResponse));
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    const editor = await screen.findByRole("textbox", {
      name: "Document editor",
    });
    expect(editor).toHaveValue("Hello");
    fireEvent.change(editor, { target: { value: "Hello\nworld" } });
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some((call) => call[1]?.method === "PUT"),
      ).toBe(true);
    });
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  test("keeps the draft visible when saving encounters a revision conflict", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (init?.method === "PUT") {
          return new Response(
            JSON.stringify({
              current_revision: "sha256:new",
              error: "revision_conflict",
              ok: false,
              schema: 1,
            }),
            { status: 409 },
          );
        }
        if (url.includes("/api/workspace/tree"))
          return new Response(JSON.stringify(treeResponse));
        if (url.includes("/api/workspace/git/status"))
          return new Response(JSON.stringify({ configured: false, schema: 1 }));
        return new Response(JSON.stringify(fileResponse));
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
    fireEvent.click(await screen.findByRole("button", { name: "notes.md" }));
    const editor = await screen.findByRole("textbox", {
      name: "Document editor",
    });
    fireEvent.change(editor, { target: { value: "draft" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Revision conflict",
    );
    expect(editor).toHaveValue("draft");
  });

  test("shows the login gate and loads the workspace after authentication", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.includes("/api/auth/session")) {
          return new Response(
            JSON.stringify({
              auth_required: true,
              authenticated: false,
              schema: 1,
            }),
          );
        }
        if (url.includes("/api/auth/login")) {
          expect(init?.method).toBe("POST");
          return new Response(JSON.stringify({ ok: true, schema: 1 }), {
            headers: { "Set-Cookie": "openlia_workspace_session=test" },
          });
        }
        if (url.includes("/api/workspace/tree"))
          return new Response(JSON.stringify(treeResponse));
        if (url.includes("/api/workspace/git/status"))
          return new Response(JSON.stringify({ configured: false, schema: 1 }));
        return new Response(JSON.stringify(fileResponse));
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<App />);
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
