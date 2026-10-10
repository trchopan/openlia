import { useEffect, useRef } from "react";
import type { CalendarEvent, CalendarSource } from "./types";

export interface WeekViewProps {
  currentDate: Date;
  events: CalendarEvent[];
  sources: CalendarSource[];
  onSelectEvent: (event: CalendarEvent) => void;
  onNewEventOnTime: (dateIso: string, timeStr?: string) => void;
}

const HOUR_HEIGHT = 56; // pixels per hour
const HOURS = Array.from({ length: 24 }, (_, i) => i);
const WEEKDAY_NAMES = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

export function WeekView({
  currentDate,
  events,
  sources,
  onSelectEvent,
  onNewEventOnTime: _onNewEventOnTime,
}: WeekViewProps) {
  const scrollContainerRef = useRef<HTMLDivElement>(null);

  // Scroll to ~08:00 on mount
  useEffect(() => {
    if (scrollContainerRef.current) {
      scrollContainerRef.current.scrollTop = 8 * HOUR_HEIGHT - 20;
    }
  }, []);

  const todayIso = new Date().toISOString().slice(0, 10);

  // Compute Monday of the current week
  const curr = new Date(
    Date.UTC(
      currentDate.getFullYear(),
      currentDate.getMonth(),
      currentDate.getDate(),
    ),
  );
  let dayOfWeek = curr.getUTCDay();
  if (dayOfWeek === 0) dayOfWeek = 7; // Sunday = 7
  const monday = new Date(
    curr.getTime() - (dayOfWeek - 1) * 24 * 60 * 60 * 1000,
  );

  // Build the 7 days of this week
  const weekDays = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(monday.getTime() + i * 24 * 60 * 60 * 1000);
    const iso = d.toISOString().slice(0, 10);
    return {
      date: d,
      iso,
      name: WEEKDAY_NAMES[i],
      monthDay: `${d.getUTCMonth() + 1}/${d.getUTCDate()}`,
      isToday: iso === todayIso,
    };
  });

  // Split into timed events and untimed events per day
  const timedByDate = new Map<string, CalendarEvent[]>();
  const untimedByDate = new Map<string, CalendarEvent[]>();

  for (const day of weekDays) {
    timedByDate.set(day.iso, []);
    untimedByDate.set(day.iso, []);
  }

  for (const ev of events) {
    if (ev.time) {
      timedByDate.get(ev.date)?.push(ev);
    } else {
      untimedByDate.get(ev.date)?.push(ev);
    }
  }

  const sourceMap = new Map<string, CalendarSource>();
  for (const src of sources) {
    sourceMap.set(src.path, src);
  }

  // Calculate current time line for today
  const now = new Date();
  const currentMinutes = now.getHours() * 60 + now.getMinutes();
  const currentTimeTop = (currentMinutes / 60) * HOUR_HEIGHT;

  return (
    <div className="flex h-full flex-col overflow-hidden bg-base-100">
      {/* Week Header (Sticky Days) */}
      <div className="flex border-b border-base-content/10 bg-base-200/50">
        {/* Time gutter spacer */}
        <div className="w-16 shrink-0 border-r border-base-content/10 py-2 text-center text-xs text-base-content/50">
          Time
        </div>

        {/* 7 Day Headers */}
        <div className="grid flex-1 grid-cols-7 divide-x divide-base-content/10">
          {weekDays.map((day) => (
            <div
              key={day.iso}
              className={`flex flex-col items-center py-2 text-center ${
                day.isToday ? "bg-primary/5" : ""
              }`}
            >
              <span className="text-xs font-semibold text-base-content/70">
                {day.name}
              </span>
              <span
                className={`mt-0.5 flex h-6 w-6 items-center justify-center rounded-full text-xs font-bold ${
                  day.isToday
                    ? "bg-primary text-primary-content"
                    : "text-base-content"
                }`}
              >
                {day.date.getUTCDate()}
              </span>

              {/* Untimed / All-day badges */}
              {untimedByDate.get(day.iso)?.length ? (
                <div className="mt-1 flex w-full flex-col gap-1 px-1">
                  {untimedByDate.get(day.iso)?.map((ev) => (
                    <button
                      key={ev.id}
                      type="button"
                      className="truncate rounded bg-base-300 px-1 py-0.5 text-left text-[11px] font-medium text-base-content hover:opacity-80"
                      onClick={() => onSelectEvent(ev)}
                    >
                      {ev.summary}
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
          ))}
        </div>
      </div>

      {/* Hourly Scroll Area */}
      <div ref={scrollContainerRef} className="flex-1 overflow-y-auto relative">
        <div className="flex" style={{ height: `${24 * HOUR_HEIGHT}px` }}>
          {/* Time Gutter */}
          <div className="w-16 shrink-0 select-none border-r border-base-content/10 bg-base-200/20 text-right pr-2 text-xs text-base-content/50">
            {HOURS.map((hour) => (
              <div
                key={hour}
                className="relative"
                style={{ height: `${HOUR_HEIGHT}px` }}
              >
                <span className="relative -top-2.5">
                  {hour.toString().padStart(2, "0")}:00
                </span>
              </div>
            ))}
          </div>

          {/* 7 Columns */}
          <div className="grid flex-1 grid-cols-7 divide-x divide-base-content/10 relative">
            {/* Horizontal Grid Lines */}
            <div className="pointer-events-none absolute inset-0 z-0 flex flex-col">
              {HOURS.map((hour) => (
                <div
                  key={hour}
                  className="border-b border-base-content/5"
                  style={{ height: `${HOUR_HEIGHT}px` }}
                />
              ))}
            </div>

            {/* Day Columns */}
            {weekDays.map((day) => {
              const dayTimedEvents = timedByDate.get(day.iso) || [];

              return (
                <div
                  key={day.iso}
                  className={`relative z-10 h-full ${day.isToday ? "bg-primary/[0.02]" : ""}`}
                >
                  {/* Today current time marker line */}
                  {day.isToday && (
                    <div
                      className="pointer-events-none absolute inset-x-0 z-30 flex items-center"
                      style={{ top: `${currentTimeTop}px` }}
                    >
                      <div className="h-2 w-2 rounded-full bg-error -ml-1" />
                      <div className="h-0.5 flex-1 bg-error" />
                    </div>
                  )}

                  {/* Timed Event Blocks */}
                  {dayTimedEvents.map((ev) => {
                    const [hStr, mStr] = (ev.time || "00:00").split(":");
                    const startMin =
                      parseInt(hStr ?? "0", 10) * 60 +
                      parseInt(mStr ?? "0", 10);
                    const top = (startMin / 60) * HOUR_HEIGHT;
                    const durationMins = ev.duration > 0 ? ev.duration : 60;
                    const height = Math.max(
                      26,
                      (durationMins / 60) * HOUR_HEIGHT - 2,
                    );

                    const endMin = startMin + durationMins;
                    const endHours = Math.floor(endMin / 60) % 24;
                    const endMins = endMin % 60;
                    const endTimeStr = `${endHours.toString().padStart(2, "0")}:${endMins.toString().padStart(2, "0")}`;

                    const src = sourceMap.get(ev.sourceFile);
                    const bg = src?.bgClass || "bg-primary/20";
                    const border = src?.borderClass || "border-primary/50";
                    const text = src?.textClass || "text-primary";

                    return (
                      <button
                        key={ev.id}
                        type="button"
                        className={`absolute inset-x-1 z-20 flex flex-col justify-between overflow-hidden rounded-md border p-1.5 shadow-xs transition-shadow hover:shadow-md cursor-pointer text-left ${bg} ${border} ${text}`}
                        style={{ top: `${top}px`, height: `${height}px` }}
                        onClick={(e) => {
                          e.stopPropagation();
                          onSelectEvent(ev);
                        }}
                      >
                        <div className="flex items-start justify-between gap-1 overflow-hidden">
                          <div className="truncate">
                            <div className="text-xs font-bold leading-tight truncate">
                              {ev.summary}
                            </div>
                            <div className="text-[10px] opacity-80 font-medium">
                              {ev.time} - {endTimeStr}
                            </div>
                          </div>

                          {/* Quick 1-click video call button */}
                          {ev.callUrl && (
                            <a
                              href={ev.callUrl}
                              target="_blank"
                              rel="noreferrer"
                              className="btn btn-xs btn-circle btn-primary h-5 w-5 min-h-0 shrink-0 shadow-xs"
                              title="Join Video Meeting"
                              onClick={(e) => e.stopPropagation()}
                            >
                              <span className="text-[10px]">📹</span>
                            </a>
                          )}
                        </div>

                        {/* Location / Attendees footnote if space permits */}
                        {height > 50 &&
                          (ev.location || ev.attendees.length > 0) && (
                            <div className="flex items-center gap-2 truncate text-[10px] opacity-75 mt-0.5">
                              {ev.location && (
                                <span className="truncate">
                                  📍 {ev.location}
                                </span>
                              )}
                              {ev.attendees.length > 0 && (
                                <span className="shrink-0">
                                  👥 {ev.attendees.length}
                                </span>
                              )}
                            </div>
                          )}
                      </button>
                    );
                  })}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
