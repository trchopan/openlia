import { describe, expect, test } from "vitest";
import { diffLines } from "./markdown";

describe("workspace markdown helpers", () => {
  test("shows changed lines without interpreting user content as HTML", () => {
    expect(diffLines("same\nold", "same\nnew")).toEqual([
      "  same",
      "- old",
      "+ new",
    ]);
  });

  test("does not show a diff for an unchanged document", () => {
    expect(diffLines("same\ncontent", "same\ncontent")).toEqual([]);
  });
});
