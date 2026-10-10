import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { CalendarPreviewPane } from "./CalendarPreviewPane";

const SAMPLE_REM_CONTENT = `# OpenLia Calendar
REM 2026-10-15 AT 10:00 DURATION 60 \\
    INFO "Location: Room 101" \\
    INFO "Video-Link: https://meet.google.com/test-meet" \\
    INFO "Attendees: Alice, Bob" \\
    INFO "Description: Review sprint goals" \\
    MSG Sprint Planning Meeting

REM Mon AT 09:00 DURATION 30 MSG Weekly Sync
`;

describe("CalendarPreviewPane", () => {
  test("renders Month view by default and displays event cards", () => {
    render(
      <CalendarPreviewPane
        content={SAMPLE_REM_CONTENT}
        filePath="calendar/reminders.rem"
      />,
    );

    // Month view is active
    expect(screen.getByRole("button", { name: "Month" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByText("Sprint Planning Meeting")).toBeInTheDocument();
  });

  test("switches between Month, Week, and Gantt views", () => {
    render(
      <CalendarPreviewPane
        content={SAMPLE_REM_CONTENT}
        filePath="calendar/reminders.rem"
      />,
    );

    // Switch to Week view
    fireEvent.click(screen.getByRole("button", { name: "Week" }));
    expect(screen.getByRole("button", { name: "Week" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    // Switch to Gantt view
    fireEvent.click(screen.getByRole("button", { name: "Gantt" }));
    expect(screen.getByRole("button", { name: "Gantt" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByText(/Gantt Schedule/)).toBeInTheDocument();
  });

  test("opens EventModal when clicking an event", () => {
    render(
      <CalendarPreviewPane
        content={SAMPLE_REM_CONTENT}
        filePath="calendar/reminders.rem"
      />,
    );

    const eventBtn = screen.getByText("Sprint Planning Meeting");
    fireEvent.click(eventBtn);

    // Inspector dialog opens
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Room 101")).toBeInTheDocument();
    expect(screen.getByText("Join Meeting")).toBeInTheDocument();
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });

  test("adds a new event and invokes onDraftChange", () => {
    const handleDraftChange = vi.fn();
    render(
      <CalendarPreviewPane
        content={SAMPLE_REM_CONTENT}
        filePath="calendar/reminders.rem"
        onDraftChange={handleDraftChange}
      />,
    );

    // Click New Event
    fireEvent.click(screen.getByRole("button", { name: "New Event" }));

    expect(
      screen.getByRole("heading", { name: "New Calendar Event" }),
    ).toBeInTheDocument();

    const titleInput = screen.getByPlaceholderText(
      /Sprint Planning, Team Standup/,
    );
    fireEvent.change(titleInput, { target: { value: "New Design Review" } });

    fireEvent.click(screen.getByRole("button", { name: "Add Event" }));

    expect(handleDraftChange).toHaveBeenCalledTimes(1);
    const updatedContent = handleDraftChange.mock.calls[0]?.[0] ?? "";
    expect(updatedContent).toContain("MSG New Design Review");
  });
});
