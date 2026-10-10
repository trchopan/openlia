import { useState } from "react";
import type { CalendarEvent, CalendarSource } from "./types";

export interface GanttViewProps {
  currentDate: Date;
  events: CalendarEvent[];
  sources: CalendarSource[];
  onSelectEvent: (event: CalendarEvent) => void;
  onNewEventOnDate: (dateIso: string) => void;
}

const DAY_WIDTH = 120; // pixels per day on Gantt timeline

export function GanttView({
  currentDate,
  events,
  sources,
  onSelectEvent,
  onNewEventOnDate: _onNewEventOnDate,
}: GanttViewProps) {
  const [rangeDays, setRangeDays] = useState<7 | 14 | 30>(14);

  const todayIso = new Date().toISOString().slice(0, 10);

  // Compute start date of Gantt timeline (e.g. Monday of current week or first of month)
  const curr = new Date(
    Date.UTC(
      currentDate.getFullYear(),
      currentDate.getMonth(),
      currentDate.getDate(),
    ),
  );
  let dayOfWeek = curr.getUTCDay();
  if (dayOfWeek === 0) dayOfWeek = 7;
  const startDay = new Date(
    curr.getTime() - (dayOfWeek - 1) * 24 * 60 * 60 * 1000,
  );

  const days = Array.from({ length: rangeDays }, (_, i) => {
    const d = new Date(startDay.getTime() + i * 24 * 60 * 60 * 1000);
    const iso = d.toISOString().slice(0, 10);
    const dayName = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"][
      d.getUTCDay()
    ];
    return {
      date: d,
      iso,
      dayName,
      monthDay: `${d.getUTCMonth() + 1}/${d.getUTCDate()}`,
      isToday: iso === todayIso,
      index: i,
    };
  });

  const firstDay = days[0];
  const lastDay = days[days.length - 1];
  const startIso = firstDay ? firstDay.iso : "";
  const endIso = lastDay ? lastDay.iso : "";

  // Filter events within range
  const visibleEvents = events
    .filter((ev) => ev.date >= startIso && ev.date <= endIso)
    .sort((a, b) => {
      if (a.date !== b.date) return a.date.localeCompare(b.date);
      const timeA = a.time || "00:00";
      const timeB = b.time || "00:00";
      return timeA.localeCompare(timeB);
    });

  const sourceMap = new Map<string, CalendarSource>();
  for (const src of sources) {
    sourceMap.set(src.path, src);
  }

  // Calculate day index map
  const dayIndexMap = new Map<string, number>();
  for (const d of days) {
    dayIndexMap.set(d.iso, d.index);
  }

  const totalWidth = rangeDays * DAY_WIDTH;

  return (
    <div className="flex h-full flex-col overflow-hidden bg-base-100">
      {/* Gantt Control Sub-toolbar */}
      <div className="flex items-center justify-between border-b border-base-content/10 bg-base-200/30 px-4 py-1.5 text-xs">
        <span className="font-semibold text-base-content/70">
          Gantt Schedule ({visibleEvents.length} events)
        </span>
        <div className="join border border-base-content/15 rounded-md bg-base-100">
          <button
            type="button"
            className={`btn btn-xs join-item ${rangeDays === 7 ? "btn-primary" : "btn-ghost"}`}
            onClick={() => setRangeDays(7)}
          >
            7 Days
          </button>
          <button
            type="button"
            className={`btn btn-xs join-item ${rangeDays === 14 ? "btn-primary" : "btn-ghost"}`}
            onClick={() => setRangeDays(14)}
          >
            14 Days
          </button>
          <button
            type="button"
            className={`btn btn-xs join-item ${rangeDays === 30 ? "btn-primary" : "btn-ghost"}`}
            onClick={() => setRangeDays(30)}
          >
            30 Days
          </button>
        </div>
      </div>

      {/* Main Gantt Body */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left Table: Event Details */}
        <div className="w-72 shrink-0 border-r border-base-content/10 flex flex-col bg-base-100 overflow-y-auto">
          {/* Header */}
          <div className="h-12 border-b border-base-content/10 bg-base-200/50 px-3 flex items-center font-semibold text-xs text-base-content/70">
            Event / Task
          </div>

          {/* Event Rows */}
          {visibleEvents.length === 0 ? (
            <div className="p-4 text-center text-xs text-base-content/50 italic">
              No events scheduled in this period.
            </div>
          ) : (
            visibleEvents.map((ev) => {
              const src = sourceMap.get(ev.sourceFile);
              return (
                <button
                  type="button"
                  key={ev.id}
                  className="h-10 border-b border-base-content/5 px-3 flex items-center justify-between gap-1 hover:bg-base-200/50 cursor-pointer transition-colors w-full text-left"
                  onClick={() => onSelectEvent(ev)}
                >
                  <div className="truncate flex items-center gap-1.5">
                    <span
                      className={`h-2 w-2 rounded-full shrink-0 ${src?.bgClass || "bg-primary"}`}
                    />
                    <span
                      className="text-xs font-medium text-base-content truncate"
                      title={ev.summary}
                    >
                      {ev.summary}
                    </span>
                  </div>
                  <div className="shrink-0 text-[10px] text-base-content/60 font-mono">
                    {ev.time || "all day"}
                  </div>
                </button>
              );
            })
          )}
        </div>

        {/* Right Area: Timeline Header & Horizontal Bars */}
        <div className="flex-1 overflow-x-auto overflow-y-auto relative">
          <div
            style={{ width: `${totalWidth}px` }}
            className="min-h-full flex flex-col"
          >
            {/* Timeline Header (Days) */}
            <div className="h-12 border-b border-base-content/10 bg-base-200/50 flex sticky top-0 z-20">
              {days.map((day) => (
                <div
                  key={day.iso}
                  className={`flex flex-col items-center justify-center border-r border-base-content/10 text-xs shrink-0 ${
                    day.isToday ? "bg-primary/10 font-bold" : ""
                  }`}
                  style={{ width: `${DAY_WIDTH}px` }}
                >
                  <span className="text-[10px] opacity-70 uppercase tracking-wide">
                    {day.dayName}
                  </span>
                  <span
                    className={
                      day.isToday ? "text-primary" : "text-base-content"
                    }
                  >
                    {day.monthDay}
                  </span>
                </div>
              ))}
            </div>

            {/* Timeline Grid & Event Bars */}
            <div className="relative flex-1">
              {/* Vertical Day Lines */}
              <div className="pointer-events-none absolute inset-0 flex z-0">
                {days.map((day) => (
                  <div
                    key={day.iso}
                    className={`border-r border-base-content/5 h-full shrink-0 ${
                      day.isToday ? "bg-primary/[0.02]" : ""
                    }`}
                    style={{ width: `${DAY_WIDTH}px` }}
                  />
                ))}
              </div>

              {/* Rows with Horizontal Duration Bars */}
              {visibleEvents.map((ev) => {
                const dayIndex = dayIndexMap.get(ev.date) ?? -1;
                if (dayIndex === -1) return null;

                // Position calculation
                let leftOffset = dayIndex * DAY_WIDTH;
                let barWidth = DAY_WIDTH * 0.9;

                if (ev.time) {
                  const parts = ev.time.split(":");
                  const h = parseInt(parts[0] ?? "0", 10) || 0;
                  const m = parseInt(parts[1] ?? "0", 10) || 0;
                  const minutesIntoDay = h * 60 + m;
                  leftOffset += (minutesIntoDay / 1440) * DAY_WIDTH;

                  const durMins = ev.duration > 0 ? ev.duration : 60;
                  barWidth = Math.max(30, (durMins / 1440) * DAY_WIDTH);
                }

                const src = sourceMap.get(ev.sourceFile);
                const bg = src?.bgClass || "bg-primary/20";
                const border = src?.borderClass || "border-primary/50";
                const text = src?.textClass || "text-primary";

                return (
                  <div
                    key={ev.id}
                    className="h-10 border-b border-base-content/5 relative flex items-center z-10"
                  >
                    <button
                      type="button"
                      className={`absolute h-7 rounded-md border shadow-xs px-2 flex items-center gap-1.5 cursor-pointer hover:shadow-md transition-shadow truncate text-left ${bg} ${border} ${text}`}
                      style={{
                        left: `${leftOffset}px`,
                        width: `${barWidth}px`,
                        minWidth: "60px",
                      }}
                      onClick={() => onSelectEvent(ev)}
                      title={`${ev.summary} (${ev.date}${ev.time ? ` at ${ev.time}` : ""})`}
                    >
                      {ev.callUrl && (
                        <span className="text-[10px] shrink-0">📹</span>
                      )}
                      <span className="text-xs font-semibold truncate leading-none">
                        {ev.summary}
                      </span>
                      {ev.duration > 0 && (
                        <span className="text-[10px] opacity-75 shrink-0 ml-auto font-mono">
                          {ev.duration}m
                        </span>
                      )}
                    </button>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
