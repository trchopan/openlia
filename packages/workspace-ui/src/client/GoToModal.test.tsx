import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { SkillSummary, WorkspaceTreeEntry } from "../shared/api";
import { GoToModal } from "./GoToModal";

const mockSkills: SkillSummary[] = [
  {
    bundled: true,
    category: "",
    description: "Conduct a weekly review of projects and goals",
    enabled: true,
    fileCount: 1,
    id: "weekly-review",
    name: "weekly-review",
    pinned: true,
    tags: [],
    useCount: 0,
  },
  {
    bundled: false,
    category: "productivity",
    description: "Notion integration",
    enabled: true,
    fileCount: 1,
    id: "productivity/notion",
    name: "notion",
    pinned: false,
    tags: [],
    useCount: 0,
  },
];

const mockTree: WorkspaceTreeEntry[] = [
  {
    editable: true,
    kind: "file",
    modified_at: "2026-09-22T00:00:00Z",
    path: "goals/career.md",
    size: 100,
  },
  {
    editable: true,
    kind: "file",
    modified_at: "2026-09-22T00:00:00Z",
    path: "projects/website.md",
    size: 100,
  },
  { kind: "directory", path: "projects" },
];

describe("GoToModal", () => {
  it("does not render when isOpen is false", () => {
    const { container } = render(
      <GoToModal
        isOpen={false}
        onClose={vi.fn()}
        onNavigateSkill={vi.fn()}
        onNavigateWorkspace={vi.fn()}
        skills={mockSkills}
        tree={mockTree}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders input and autofocuses when isOpen is true", () => {
    render(
      <GoToModal
        isOpen={true}
        onClose={vi.fn()}
        onNavigateSkill={vi.fn()}
        onNavigateWorkspace={vi.fn()}
        skills={mockSkills}
        tree={mockTree}
      />,
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(
      screen.getByPlaceholderText(
        "Paste link or path, or type file/skill name...",
      ),
    ).toBeInTheDocument();
  });

  it("navigates to workspace document on paste of openlia://workspace/... and enter", () => {
    const onNavigateWorkspace = vi.fn();
    const onNavigateSkill = vi.fn();
    const onClose = vi.fn();

    render(
      <GoToModal
        isOpen={true}
        onClose={onClose}
        onNavigateSkill={onNavigateSkill}
        onNavigateWorkspace={onNavigateWorkspace}
        skills={mockSkills}
        tree={mockTree}
      />,
    );

    const input = screen.getByPlaceholderText(
      "Paste link or path, or type file/skill name...",
    );
    fireEvent.change(input, {
      target: {
        value: "https://workspace.example.com/files/projects/website.md",
      },
    });

    expect(screen.getByText("Document")).toBeInTheDocument();
    expect(screen.getByText("projects/website.md")).toBeInTheDocument();

    fireEvent.keyDown(input, { key: "Enter" });

    expect(onNavigateWorkspace).toHaveBeenCalledWith("projects/website.md");
    expect(onNavigateSkill).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it("navigates to skill on paste of openlia://skills/... and clicking open", () => {
    const onNavigateWorkspace = vi.fn();
    const onNavigateSkill = vi.fn();
    const onClose = vi.fn();

    render(
      <GoToModal
        isOpen={true}
        onClose={onClose}
        onNavigateSkill={onNavigateSkill}
        onNavigateWorkspace={onNavigateWorkspace}
        skills={mockSkills}
        tree={mockTree}
      />,
    );

    const input = screen.getByPlaceholderText(
      "Paste link or path, or type file/skill name...",
    );
    fireEvent.change(input, {
      target: { value: "openlia://skills/weekly-review" },
    });

    expect(screen.getByText("Skill")).toBeInTheDocument();
    expect(screen.getByText("weekly-review")).toBeInTheDocument();

    const openButton = screen.getByRole("button", { name: /Open/i });
    fireEvent.click(openButton);

    expect(onNavigateSkill).toHaveBeenCalledWith("weekly-review", undefined);
    expect(onClose).toHaveBeenCalled();
  });

  it("selects suggestion via arrow keys and enter", () => {
    const onNavigateWorkspace = vi.fn();
    const onNavigateSkill = vi.fn();
    const onClose = vi.fn();

    render(
      <GoToModal
        isOpen={true}
        onClose={onClose}
        onNavigateSkill={onNavigateSkill}
        onNavigateWorkspace={onNavigateWorkspace}
        skills={mockSkills}
        tree={mockTree}
      />,
    );

    const input = screen.getByPlaceholderText(
      "Paste link or path, or type file/skill name...",
    );
    fireEvent.change(input, { target: { value: "career" } });

    // Suggestion for goals/career.md should be visible
    expect(screen.getByText("goals/career.md")).toBeInTheDocument();

    // Press ArrowDown to highlight the suggestion
    fireEvent.keyDown(input, { key: "ArrowDown" });
    // Press Enter to navigate
    fireEvent.keyDown(input, { key: "Enter" });

    expect(onNavigateWorkspace).toHaveBeenCalledWith("goals/career.md");
    expect(onClose).toHaveBeenCalled();
  });

  it("closes when Escape key is pressed", () => {
    const onClose = vi.fn();

    render(
      <GoToModal
        isOpen={true}
        onClose={onClose}
        onNavigateSkill={vi.fn()}
        onNavigateWorkspace={vi.fn()}
        skills={mockSkills}
        tree={mockTree}
      />,
    );

    const input = screen.getByPlaceholderText(
      "Paste link or path, or type file/skill name...",
    );
    fireEvent.keyDown(input, { key: "Escape" });

    expect(onClose).toHaveBeenCalled();
  });
});
