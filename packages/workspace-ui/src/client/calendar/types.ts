export type CalendarViewMode = "month" | "week" | "gantt";

export interface CalendarEvent {
  id: string;
  summary: string;
  date: string; // YYYY-MM-DD or weekday identifier
  time?: string | undefined; // HH:MM (24-hour)
  duration: number; // in minutes (0 if unspecified)
  attendees: string[];
  location?: string | undefined;
  mapUrl?: string | undefined;
  callUrl?: string | undefined;
  description?: string | undefined;
  note?: string | undefined;
  tags: string[];
  priority?: number | undefined;
  repeat?: string | undefined;
  sourceFile: string;
  lineNumber?: number | undefined;
  rawStatement?: string | undefined;
}

export interface CalendarSource {
  path: string;
  name: string;
  color: string; // hex or badge color
  bgClass: string;
  textClass: string;
  borderClass: string;
  enabled: boolean;
  isCurrentFile: boolean;
  eventCount: number;
}

export interface CalendarFilterState {
  enabledSources: Set<string>;
}
