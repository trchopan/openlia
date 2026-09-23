import { afterEach, describe, expect, test, vi } from "vitest";
import { httpWorkspaceApi } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("HTTP workspace API", () => {
  test("rejects a successful response that does not match the contract", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ auth_required: false, schema: 1 })),
      ),
    );

    await expect(httpWorkspaceApi.loadSession()).rejects.toThrow(
      "invalid_api_response",
    );
  });

  test("validates tree entries instead of accepting arbitrary arrays", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              entries: [{ path: "notes.md", kind: "file" }],
              schema: 1,
              truncated: false,
            }),
          ),
      ),
    );

    await expect(httpWorkspaceApi.loadTree()).rejects.toThrow(
      "invalid_api_response",
    );
  });

  test("sends writes through the HTTP contract", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            editable: true,
            modified_at: "2026-09-22T00:00:00.000Z",
            ok: true,
            path: "notes.md",
            revision: "sha256:after",
            schema: 1,
            size: 5,
          }),
        ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await httpWorkspaceApi.saveFile("notes.md", "Hello", "sha256:before");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workspace/file",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          content: "Hello",
          expected_revision: "sha256:before",
          path: "notes.md",
        }),
      }),
    );
  });
});
