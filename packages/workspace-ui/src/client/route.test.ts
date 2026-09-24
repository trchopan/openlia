import { describe, expect, test } from "vitest";
import { buildRouteUrl, parseRoute } from "./route";

describe("route helpers", () => {
  test("parses root overview route", () => {
    expect(parseRoute("/")).toEqual({
      filter: undefined,
      path: undefined,
      scenario: undefined,
      view: undefined,
    });
  });

  test("parses /files/ document path", () => {
    expect(parseRoute("/files/notes.md")).toEqual({
      filter: undefined,
      path: "notes.md",
      scenario: undefined,
      view: undefined,
    });
  });

  test("parses nested /files/ paths with url encoding", () => {
    expect(
      parseRoute("/files/calendar/2026-09-23%20lunch%20meeting.md"),
    ).toEqual({
      filter: undefined,
      path: "calendar/2026-09-23 lunch meeting.md",
      scenario: undefined,
      view: undefined,
    });
  });

  test("parses /file/ alias route", () => {
    expect(parseRoute("/file/tasks/todo.md")).toEqual({
      filter: undefined,
      path: "tasks/todo.md",
      scenario: undefined,
      view: undefined,
    });
  });

  test("parses query parameter fallback ?path= and ?file=", () => {
    expect(parseRoute("/?path=notes.md")).toEqual({
      filter: undefined,
      path: "notes.md",
      scenario: undefined,
      view: undefined,
    });

    expect(parseRoute("/?file=inbox/draft.md")).toEqual({
      filter: undefined,
      path: "inbox/draft.md",
      scenario: undefined,
      view: undefined,
    });
  });

  test("parses view, filter/q, and scenario params", () => {
    expect(
      parseRoute("/files/notes.md?view=preview&filter=cal&scenario=auth"),
    ).toEqual({
      filter: "cal",
      path: "notes.md",
      scenario: "auth",
      view: "preview",
    });

    expect(parseRoute("/?q=searchterm&view=split")).toEqual({
      filter: "searchterm",
      path: undefined,
      scenario: undefined,
      view: "split",
    });
  });

  test("ignores invalid view parameter", () => {
    expect(parseRoute("/files/notes.md?view=invalid_view")).toEqual({
      filter: undefined,
      path: "notes.md",
      scenario: undefined,
      view: undefined,
    });
  });

  test("builds clean URL from route object", () => {
    expect(buildRouteUrl({})).toBe("/");
    expect(buildRouteUrl({ path: "notes.md" })).toBe("/files/notes.md");
    expect(
      buildRouteUrl({
        path: "calendar/lunch meeting.md",
        view: "preview",
      }),
    ).toBe("/files/calendar/lunch%20meeting.md?view=preview");
    expect(
      buildRouteUrl({
        filter: "todo",
        path: "tasks/task.md",
        scenario: "conflict",
        view: "edit",
      }),
    ).toBe("/files/tasks/task.md?scenario=conflict&view=edit&filter=todo");
    expect(
      buildRouteUrl({
        filter: "notes",
      }),
    ).toBe("/?filter=notes");
  });
});
