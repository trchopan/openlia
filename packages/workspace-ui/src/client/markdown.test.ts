import { describe, expect, test } from "vitest";
import { diffLines, markdownBlocks } from "./markdown";

describe("workspace markdown helpers", () => {
  test("parses the supported markdown blocks", () => {
    expect(
      markdownBlocks("# Title\n\n**bold**\n> quote\n- item\n~~~\ncode\n~~~"),
    ).toEqual([
      { kind: "heading", level: 1, text: "Title" },
      { kind: "paragraph", text: "**bold**" },
      { kind: "blockquote", text: "quote" },
      { kind: "list", text: "item" },
      { kind: "code", text: "code" },
    ]);
  });

  test("shows changed lines without interpreting user content as HTML", () => {
    expect(diffLines("same\nold", "same\nnew")).toEqual([
      "  same",
      "- old",
      "+ new",
    ]);
  });
});
