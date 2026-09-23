import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import { MarkdownPreview } from "./components";

describe("workspace presentation components", () => {
  test("renders safe GFM structures semantically", () => {
    const { container } = render(
      <MarkdownPreview
        content={
          "# Notes\n\n- [x] shipped\n\n| Area | State |\n| --- | --- |\n| UI | ready |\n\n<script>alert(1)</script>"
        }
      />,
    );

    expect(screen.getByRole("heading", { name: "Notes" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox")).toBeDisabled();
    expect(screen.getByRole("table")).toBeInTheDocument();
    expect(container.querySelector("script")).toBeNull();
  });
});
