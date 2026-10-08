import { describe, expect, test } from "vitest";
import { diffLines, extractOriginalMessage } from "./markdown";

describe("workspace markdown helpers", () => {
  test("extracts the marked original message from the document body", () => {
    const result = extractOriginalMessage(
      "# Review\n\n- Candidate\n\n<!-- ORIGINAL MESSAGE START -->\n\nOriginal **message**.\n<!-- ORIGINAL MESSAGE END -->",
    );

    expect(result.body).toBe("# Review\n\n- Candidate");
    expect(result.originalMessage).toBe("Original **message**.");
  });

  test("does not interpret marker text inside a fenced code block", () => {
    const result = extractOriginalMessage(
      "# Review\n\n<!-- ORIGINAL MESSAGE START -->\n\n```md\n<!-- ORIGINAL MESSAGE END -->\n```\n\n<!-- ORIGINAL MESSAGE END -->",
    );

    expect(result.originalMessage).toBe(
      "```md\n<!-- ORIGINAL MESSAGE END -->\n```",
    );
  });

  test("keeps an incomplete original-message section readable", () => {
    const result = extractOriginalMessage(
      "# Review\n\n<!-- ORIGINAL MESSAGE START -->\n\nOriginal message",
    );

    expect(result.body).toBe("# Review");
    expect(result.originalMessage).toBe("Original message");
  });

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
