import { describe, expect, test } from "vitest";
import { buildRouteUrl, parseRoute } from "./route";

describe("route helpers", () => {
  test("parses root overview route", () => {
    expect(parseRoute("/")).toEqual({
      filter: undefined,
      path: undefined,
      scenario: undefined,
      skill: undefined,
      skillFile: undefined,
      tab: undefined,
      view: undefined,
    });
  });

  test("parses /files/ document path", () => {
    expect(parseRoute("/files/notes.md")).toEqual({
      filter: undefined,
      path: "notes.md",
      scenario: undefined,
      skill: undefined,
      skillFile: undefined,
      tab: "documents",
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
      skill: undefined,
      skillFile: undefined,
      tab: "documents",
      view: undefined,
    });
  });

  test("parses /file/ alias route", () => {
    expect(parseRoute("/file/tasks/todo.md")).toEqual({
      filter: undefined,
      path: "tasks/todo.md",
      scenario: undefined,
      skill: undefined,
      skillFile: undefined,
      tab: "documents",
      view: undefined,
    });
  });

  test("parses query parameter fallback ?path= and ?file=", () => {
    expect(parseRoute("/?path=notes.md")).toEqual({
      filter: undefined,
      path: "notes.md",
      scenario: undefined,
      skill: undefined,
      skillFile: undefined,
      tab: undefined,
      view: undefined,
    });

    expect(parseRoute("/?file=inbox/draft.md")).toEqual({
      filter: undefined,
      path: "inbox/draft.md",
      scenario: undefined,
      skill: undefined,
      skillFile: undefined,
      tab: undefined,
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
      skill: undefined,
      skillFile: undefined,
      tab: "documents",
      view: "preview",
    });

    expect(parseRoute("/?q=searchterm&view=edit")).toEqual({
      filter: "searchterm",
      path: undefined,
      scenario: undefined,
      skill: undefined,
      skillFile: undefined,
      tab: undefined,
      view: "edit",
    });
  });

  test("ignores invalid view parameter", () => {
    expect(parseRoute("/files/notes.md?view=invalid_view")).toEqual({
      filter: undefined,
      path: "notes.md",
      scenario: undefined,
      skill: undefined,
      skillFile: undefined,
      tab: "documents",
      view: undefined,
    });
    expect(parseRoute("/files/notes.md?view=split")).toEqual({
      filter: undefined,
      path: "notes.md",
      scenario: undefined,
      skill: undefined,
      skillFile: undefined,
      tab: "documents",
      view: undefined,
    });
  });

  test("parses skills routes and query params", () => {
    expect(parseRoute("/skills")).toEqual({
      filter: undefined,
      path: undefined,
      scenario: undefined,
      skill: undefined,
      skillFile: undefined,
      tab: "skills",
      view: undefined,
    });

    expect(
      parseRoute("/skills/productivity/notion?skillFile=scripts/sync.py"),
    ).toEqual({
      filter: undefined,
      path: undefined,
      scenario: undefined,
      skill: "productivity/notion",
      skillFile: "scripts/sync.py",
      tab: "skills",
      view: undefined,
    });

    expect(parseRoute("/?tab=skills&skill=claim-review")).toEqual({
      filter: undefined,
      path: undefined,
      scenario: undefined,
      skill: "claim-review",
      skillFile: undefined,
      tab: "skills",
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
    expect(
      buildRouteUrl({
        tab: "skills",
      }),
    ).toBe("/skills");
    expect(
      buildRouteUrl({
        skill: "productivity/notion",
        skillFile: "scripts/sync.py",
        tab: "skills",
      }),
    ).toBe("/skills/productivity/notion?skillFile=scripts%2Fsync.py");
  });
});
