import { describe, expect, test } from "vitest";
import {
  isChatgptExportPath,
  parseChatgptExport,
  presentationContent,
} from "./chatgpt";

const validExport = `schema: 1
session:
  platform: chatgpt
  mode: temporary
  started_at: "2026-09-24T09:24:42.563Z"
  model: ChatGPT
  topic: Structured preview
  turns_count: 2
messages:
  - role: user
    turn: 1
    content: |
      Explain this topic.
      Show more
  - role: assistant
    turn: 1
    content: |
      # Answer

      Here is the answer.
    references:
      - id: ref-1
        index: 1
        title: OpenLia
        url: https://openlia.example/docs
references:
  - id: ref-1
    index: 1
    title: OpenLia
    url: https://openlia.example/docs
    turns:
      - 1
`;

describe("ChatGPT export parsing", () => {
  test("recognizes schema-1 exports and deduplicates sources", () => {
    const parsed = parseChatgptExport(validExport);

    expect(parsed?.session.topic).toBe("Structured preview");
    expect(parsed?.messages).toHaveLength(2);
    expect(parsed?.messages[1]?.content).toContain("# Answer");
    expect(parsed?.references).toEqual([
      expect.objectContaining({
        domain: "openlia.example",
        title: "OpenLia",
        turns: [1],
        url: "https://openlia.example/docs",
      }),
    ]);
  });

  test("rejects malformed, unknown, and unsafe exports", () => {
    expect(parseChatgptExport("schema: [")).toBeNull();
    expect(
      parseChatgptExport(
        validExport.replace("platform: chatgpt", "platform: gemini"),
      ),
    ).toBeNull();
    expect(
      parseChatgptExport(
        validExport.replaceAll(
          "url: https://openlia.example/docs",
          "url: javascript:alert(1)",
        ),
      )?.references,
    ).toEqual([]);
    expect(parseChatgptExport("schema: 1\nschema: 2\n")).toBeNull();
  });

  test("recognizes only ChatGPT workspace YAML paths", () => {
    expect(isChatgptExportPath("knowledge/chatgpt/export.yaml")).toBe(true);
    expect(isChatgptExportPath("knowledge/chatgpt/export.yml")).toBe(true);
    expect(isChatgptExportPath("knowledge/gemini/export.yaml")).toBe(false);
    expect(isChatgptExportPath("knowledge/chatgpt/export.md")).toBe(false);
  });

  test("removes the extraction-only trailing Show more label", () => {
    expect(presentationContent("Answer\nShow more")).toBe("Answer");
    expect(presentationContent("Show more is part of this sentence")).toBe(
      "Show more is part of this sentence",
    );
  });
});
