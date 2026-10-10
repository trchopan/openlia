import { useEffect, useMemo, useState } from "react";
import { CalendarToolbar } from "./CalendarToolbar";
import { EventModal } from "./EventModal";
import { GanttView } from "./GanttView";
import { MonthView } from "./MonthView";
import {
  addEventToFile,
  deleteEventFromFile,
  expandEventsForRange,
  parseRemindFile,
  updateEventInFile,
} from "./remindParser";
import type { CalendarEvent, CalendarSource, CalendarViewMode } from "./types";
import { WeekView } from "./WeekView";

export interface CalendarPreviewPaneProps {
  content: string;
  filePath: string;
  onDraftChange?: ((newContent: string) => void) | undefined;
  onSwitchToEdit?: (() => void) | undefined;
  onNavigateLink?: ((path: string) => void) | undefined;
  otherCalendarFiles?: Array<{ path: string; content: string }> | undefined;
}

const COLOR_PALETTES = [
  {
    bgClass: "bg-primary/15",
    textClass: "text-primary",
    borderClass: "border-primary/40",
    color: "#3b82f6",
  },
  {
    bgClass: "bg-secondary/15",
    textClass: "text-secondary",
    borderClass: "border-secondary/40",
    color: "#ec4899",
  },
  {
    bgClass: "bg-accent/15",
    textClass: "text-accent",
    borderClass: "border-accent/40",
    color: "#10b981",
  },
  {
    bgClass: "bg-warning/15",
    textClass: "text-warning",
    borderClass: "border-warning/40",
    color: "#f59e0b",
  },
  {
    bgClass: "bg-info/15",
    textClass: "text-info",
    borderClass: "border-info/40",
    color: "#06b6d4",
  },
];

export function CalendarPreviewPane({
  content,
  filePath,
  onDraftChange,
  onSwitchToEdit,
  onNavigateLink,
  otherCalendarFiles = [],
}: CalendarPreviewPaneProps) {
  const [viewMode, setViewMode] = useState<CalendarViewMode>("month");
  const [currentDate, setCurrentDate] = useState<Date>(() => new Date());

  const [enabledSources, setEnabledSources] = useState<Set<string>>(
    () => new Set([filePath]),
  );

  // Modal state
  const [modalState, setModalState] = useState<{
    isOpen: boolean;
    mode: "view" | "edit" | "create";
    event: CalendarEvent | null;
    presetDate?: string | undefined;
    presetTime?: string | undefined;
  }>({
    isOpen: false,
    mode: "view",
    event: null,
  });

  // Ensure current file is always in enabledSources by default
  useEffect(() => {
    setEnabledSources((prev) => {
      const next = new Set(prev);
      next.add(filePath);
      return next;
    });
  }, [filePath]);

  // Parse current file
  const parsedCurrent = useMemo(() => {
    return parseRemindFile(content, filePath);
  }, [content, filePath]);

  // Parse other available files
  const parsedOthers = useMemo(() => {
    return otherCalendarFiles.map((file) => ({
      path: file.path,
      ...parseRemindFile(file.content, file.path),
    }));
  }, [otherCalendarFiles]);

  // Build Calendar Sources
  const sources: CalendarSource[] = useMemo(() => {
    const list: CalendarSource[] = [];

    // Primary current file
    const currentName =
      filePath
        .split("/")
        .pop()
        ?.replace(/\.rem(ind)?$/, "") || "Primary";
    const primaryPalette = COLOR_PALETTES[0] ?? {
      color: "#3b82f6",
      bgClass: "bg-primary/15",
      textClass: "text-primary",
      borderClass: "border-primary/40",
    };
    list.push({
      path: filePath,
      name: currentName,
      color: primaryPalette.color,
      bgClass: primaryPalette.bgClass,
      textClass: primaryPalette.textClass,
      borderClass: primaryPalette.borderClass,
      enabled: enabledSources.has(filePath),
      isCurrentFile: true,
      eventCount: parsedCurrent.events.length,
    });

    // Other files
    parsedOthers.forEach((other, index) => {
      const palette =
        COLOR_PALETTES[(index + 1) % COLOR_PALETTES.length] ?? primaryPalette;
      const otherName =
        other.path
          .split("/")
          .pop()
          ?.replace(/\.rem(ind)?$/, "") || `Calendar ${index + 1}`;
      list.push({
        path: other.path,
        name: otherName,
        color: palette.color,
        bgClass: palette.bgClass,
        textClass: palette.textClass,
        borderClass: palette.borderClass,
        enabled: enabledSources.has(other.path),
        isCurrentFile: false,
        eventCount: other.events.length,
      });
    });

    return list;
  }, [filePath, parsedCurrent.events.length, parsedOthers, enabledSources]);

  // Aggregate raw events from all enabled sources
  const allRawEvents = useMemo(() => {
    const result: CalendarEvent[] = [];
    if (enabledSources.has(filePath)) {
      result.push(...parsedCurrent.events);
    }
    for (const other of parsedOthers) {
      if (enabledSources.has(other.path)) {
        result.push(...other.events);
      }
    }
    return result;
  }, [enabledSources, filePath, parsedCurrent.events, parsedOthers]);

  // Expand events for the active visible date window
  const visibleExpandedEvents = useMemo(() => {
    const year = currentDate.getFullYear();
    const month = currentDate.getMonth();

    let startIso: string;
    let endIso: string;

    if (viewMode === "month") {
      const first = new Date(Date.UTC(year, month - 1, 20));
      const last = new Date(Date.UTC(year, month + 1, 15));
      startIso = first.toISOString().slice(0, 10);
      endIso = last.toISOString().slice(0, 10);
    } else if (viewMode === "week") {
      const d = new Date(currentDate.getTime());
      let day = d.getDay();
      if (day === 0) day = 7;
      const mon = new Date(d.getTime() - (day - 1) * 86400000);
      const sun = new Date(mon.getTime() + 6 * 86400000);
      startIso = mon.toISOString().slice(0, 10);
      endIso = sun.toISOString().slice(0, 10);
    } else {
      // Gantt: 35 days window
      startIso = new Date(currentDate.getTime() - 7 * 86400000)
        .toISOString()
        .slice(0, 10);
      endIso = new Date(currentDate.getTime() + 35 * 86400000)
        .toISOString()
        .slice(0, 10);
    }

    return expandEventsForRange(allRawEvents, startIso, endIso);
  }, [allRawEvents, currentDate, viewMode]);

  // Navigation handlers
  const handlePrev = () => {
    setCurrentDate((prev) => {
      const d = new Date(prev);
      if (viewMode === "month") {
        d.setMonth(d.getMonth() - 1);
      } else if (viewMode === "week") {
        d.setDate(d.getDate() - 7);
      } else {
        d.setDate(d.getDate() - 14);
      }
      return d;
    });
  };

  const handleNext = () => {
    setCurrentDate((prev) => {
      const d = new Date(prev);
      if (viewMode === "month") {
        d.setMonth(d.getMonth() + 1);
      } else if (viewMode === "week") {
        d.setDate(d.getDate() + 7);
      } else {
        d.setDate(d.getDate() + 14);
      }
      return d;
    });
  };

  const handleToday = () => {
    setCurrentDate(new Date());
  };

  const handleToggleSource = (path: string) => {
    setEnabledSources((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        if (next.size > 1) {
          next.delete(path);
        }
      } else {
        next.add(path);
      }
      return next;
    });
  };

  // Title generation
  const title = useMemo(() => {
    const year = currentDate.getFullYear();
    const monthName = currentDate.toLocaleString("default", { month: "long" });

    if (viewMode === "month" || viewMode === "gantt") {
      return `${monthName} ${year}`;
    }

    // Week title
    const d = new Date(currentDate.getTime());
    let day = d.getDay();
    if (day === 0) day = 7;
    const mon = new Date(d.getTime() - (day - 1) * 86400000);
    const sun = new Date(mon.getTime() + 6 * 86400000);

    const monMonth = mon.toLocaleString("default", { month: "short" });
    const sunMonth = sun.toLocaleString("default", { month: "short" });

    if (monMonth === sunMonth) {
      return `${monMonth} ${mon.getDate()} – ${sun.getDate()}, ${year}`;
    }
    return `${monMonth} ${mon.getDate()} – ${sunMonth} ${sun.getDate()}, ${year}`;
  }, [currentDate, viewMode]);

  // Saving event modifications
  const handleSaveEvent = (
    eventData: Partial<CalendarEvent>,
    targetSource: string,
  ) => {
    if (!onDraftChange) return;

    if (modalState.mode === "create") {
      if (targetSource === filePath) {
        const updatedContent = addEventToFile(content, eventData);
        onDraftChange(updatedContent);
      }
    } else if (modalState.mode === "edit" && modalState.event) {
      if (modalState.event.sourceFile === filePath) {
        const updatedContent = updateEventInFile(
          content,
          modalState.event,
          eventData,
        );
        onDraftChange(updatedContent);
      }
    }
  };

  const handleDeleteEvent = (event: CalendarEvent) => {
    if (!onDraftChange) return;
    if (event.sourceFile === filePath) {
      const updatedContent = deleteEventFromFile(content, event);
      onDraftChange(updatedContent);
    }
  };

  return (
    <div className="flex h-full flex-col overflow-hidden bg-base-100">
      {/* Top Calendar Toolbar */}
      <CalendarToolbar
        viewMode={viewMode}
        onViewModeChange={setViewMode}
        title={title}
        onPrev={handlePrev}
        onNext={handleNext}
        onToday={handleToday}
        sources={sources}
        onToggleSource={handleToggleSource}
        onNewEvent={() =>
          setModalState({
            isOpen: true,
            mode: "create",
            event: null,
            presetDate: currentDate.toISOString().slice(0, 10),
          })
        }
        onSwitchToEdit={onSwitchToEdit}
      />

      {/* Active Calendar View */}
      <div className="flex-1 overflow-hidden">
        {viewMode === "month" && (
          <MonthView
            currentDate={currentDate}
            events={visibleExpandedEvents}
            sources={sources}
            onSelectEvent={(ev) =>
              setModalState({ isOpen: true, mode: "view", event: ev })
            }
            onNewEventOnDate={(dateIso) =>
              setModalState({
                isOpen: true,
                mode: "create",
                event: null,
                presetDate: dateIso,
              })
            }
          />
        )}

        {viewMode === "week" && (
          <WeekView
            currentDate={currentDate}
            events={visibleExpandedEvents}
            sources={sources}
            onSelectEvent={(ev) =>
              setModalState({ isOpen: true, mode: "view", event: ev })
            }
            onNewEventOnTime={(dateIso, timeStr) =>
              setModalState({
                isOpen: true,
                mode: "create",
                event: null,
                presetDate: dateIso,
                presetTime: timeStr,
              })
            }
          />
        )}

        {viewMode === "gantt" && (
          <GanttView
            currentDate={currentDate}
            events={visibleExpandedEvents}
            sources={sources}
            onSelectEvent={(ev) =>
              setModalState({ isOpen: true, mode: "view", event: ev })
            }
            onNewEventOnDate={(dateIso) =>
              setModalState({
                isOpen: true,
                mode: "create",
                event: null,
                presetDate: dateIso,
              })
            }
          />
        )}
      </div>

      {/* Event Details & Editor Modal */}
      {modalState.isOpen && (
        <EventModal
          event={modalState.event}
          mode={modalState.mode}
          presetDate={modalState.presetDate}
          presetTime={modalState.presetTime}
          sources={sources}
          currentFilePath={filePath}
          onClose={() =>
            setModalState({ isOpen: false, mode: "view", event: null })
          }
          onSave={handleSaveEvent}
          onDelete={handleDeleteEvent}
          onNavigateLink={onNavigateLink}
        />
      )}
    </div>
  );
}
