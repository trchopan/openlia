import { describe, expect, test } from "bun:test";
import {
  buildConversationYaml,
  buildRouteYaml,
  cleanUrl,
  currentTabIndex,
  mapsRouteUrl,
  McpClient,
} from "./browser";

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

  test("tracks the explicit current tab without relying on tab order", () => {
    expect(
      currentTabIndex(
        "- 0: https://maps.google.com/\n- 4: https://chatgpt.com/ (current)\n",
      ),
    ).toBe(4);
  });

  test("builds a queued Maps route URL and YAML result", () => {
    const url = mapsRouteUrl("Home", "Office", "driving");
    expect(url).toContain("origin=Home");
    expect(url).toContain("destination=Office");
    expect(url).toContain("travelmode=driving");
    const output = buildRouteYaml(
      {
        start: "Home",
        destination: "Office",
        mode: "driving",
        url,
        snapshot: "45 min\nHeavy traffic",
      },
      "2026-09-23T00:00:00.000Z",
    );
    expect(output).toContain("type: google_maps_route");
    expect(output).toContain('observed_at: "2026-09-23T00:00:00.000Z"');
    expect(output).toContain("Heavy traffic");
  });
});
