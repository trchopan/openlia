import { describe, expect, test } from "bun:test";
import { McpClient, buildConversationYaml, cleanUrl } from "./browser";

describe("browser compatibility helpers", () => {
  test("normalizes the MCP host header independently of the relay hostname", () => {
    expect(new McpClient("http://locho-browser:9000").host).toBe(
      "localhost:9000",
    );
    expect(new McpClient("http://remote-browser").host).toBe("localhost:8931");
  });

  test("builds a schema-1 conversation with deduplicated references", () => {
    const output = buildConversationYaml(
      {
        platform: "gemini",
        topic: "test",
        model: "Gemini",
        url: "https://gemini.google.com/app",
      },
      [
        { role: "user", turn: 1, content: "Question", references: [] },
        {
          role: "assistant",
          turn: 1,
          content: "Answer\nwith detail",
          references: [
            { title: "Docs", url: "https://example.test/page?utm_source=test" },
            { title: "Docs", url: "https://example.test/page" },
          ],
        },
      ],
      "2026-09-22T00:00:00.000000+00:00",
    );
    expect(output).toContain("schema: 1");
    expect(output).toContain("platform: gemini");
    expect(output).toContain("mode: temporary");
    expect(output).toContain("https://example.test/page");
    expect(output).not.toContain("utm_source");
    expect(output).toContain("content: |");
  });

  test("unwraps Google citation redirects before sanitizing tracking fields", () => {
    expect(
      cleanUrl(
        "https://www.google.com/url?q=https%3A%2F%2Fexample.test%2Fdocs%3Futm_source%3Dgoogle&sa=U",
      ),
    ).toBe("https://example.test/docs");
  });
});
