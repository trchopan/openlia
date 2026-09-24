import { parseDocument } from "yaml";

const MAX_CHAT_EXPORT_BYTES = 2 * 1024 * 1024;
const MAX_MESSAGES = 100;
const MAX_REFERENCES = 500;
const MAX_MESSAGE_CHARS = 500_000;

export interface ChatMessage {
  content: string;
  references: string[];
  role: string;
  turn?: number;
}

export interface ChatReference {
  domain: string;
  id: string;
  index: number;
  siteName: string;
  title: string;
  turns: number[];
  url: string;
}

export interface ChatExport {
  messages: ChatMessage[];
  references: ChatReference[];
  session: {
    mode?: string;
    model?: string;
    platform: "chatgpt";
    startedAt?: string;
    topic?: string;
    turnsCount?: number;
    url?: string;
  };
}

function record(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function stringValue(
  value: unknown,
  maxLength = MAX_MESSAGE_CHARS,
): string | undefined {
  return typeof value === "string" && value.length <= maxLength
    ? value
    : undefined;
}

function numberValue(value: unknown): number | undefined {
  return typeof value === "number" && Number.isSafeInteger(value)
    ? value
    : undefined;
}

function safeUrl(value: unknown): string | undefined {
  const raw = stringValue(value, 8_192);
  if (!raw) return undefined;
  try {
    const url = new URL(raw);
    return url.protocol === "http:" || url.protocol === "https:"
      ? url.href
      : undefined;
  } catch {
    return undefined;
  }
}

function referenceUrl(value: unknown): string | undefined {
  const url = safeUrl(value);
  if (!url) return undefined;
  try {
    return new URL(url).href;
  } catch {
    return undefined;
  }
}

function referenceFrom(
  value: unknown,
  fallbackIndex: number,
): ChatReference | null {
  const source = record(value);
  if (!source) return null;
  const url = referenceUrl(source.url);
  if (!url) return null;
  const parsedUrl = new URL(url);
  const title = stringValue(source.title, 2_000) ?? parsedUrl.hostname;
  const siteName = stringValue(source.site_name, 2_000) ?? parsedUrl.hostname;
  const turns = Array.isArray(source.turns)
    ? source.turns.filter(
        (turn): turn is number => numberValue(turn) !== undefined,
      )
    : [];
  return {
    domain: parsedUrl.hostname,
    id: stringValue(source.id, 200) ?? `ref-${fallbackIndex}`,
    index: numberValue(source.index) ?? fallbackIndex,
    siteName,
    title,
    turns: [...new Set(turns)],
    url,
  };
}

function addReference(
  references: Map<string, ChatReference>,
  value: unknown,
  turn: number | undefined,
): void {
  if (references.size >= MAX_REFERENCES) return;
  const reference = referenceFrom(value, references.size + 1);
  if (!reference) return;
  const current = references.get(reference.url);
  if (current) {
    if (turn !== undefined && !current.turns.includes(turn))
      current.turns.push(turn);
    return;
  }
  if (turn !== undefined && !reference.turns.includes(turn))
    reference.turns.push(turn);
  references.set(reference.url, reference);
}

function parseMessages(value: unknown): ChatMessage[] | null {
  if (
    !Array.isArray(value) ||
    value.length === 0 ||
    value.length > MAX_MESSAGES
  )
    return null;
  const messages: ChatMessage[] = [];
  for (const item of value) {
    const source = record(item);
    const role = source ? stringValue(source.role, 100) : undefined;
    const content = source ? stringValue(source.content) : undefined;
    if (!role || content === undefined) return null;
    const turn = source ? numberValue(source.turn) : undefined;
    const references =
      source && Array.isArray(source.references)
        ? source.references
            .map((reference) => referenceFrom(reference, 0)?.url)
            .filter((url): url is string => url !== undefined)
        : [];
    const message: ChatMessage = { content, references, role };
    if (turn !== undefined) message.turn = turn;
    messages.push(message);
  }
  return messages;
}

export function isChatgptExportPath(path: string): boolean {
  const normalized = path.replaceAll("\\", "/").toLowerCase();
  return (
    normalized.startsWith("knowledge/chatgpt/") &&
    (normalized.endsWith(".yaml") || normalized.endsWith(".yml"))
  );
}

export function presentationContent(content: string): string {
  return content.replace(/(?:\r?\n)?Show more\s*$/, "").trimEnd();
}

export function parseChatgptExport(content: string): ChatExport | null {
  if (new TextEncoder().encode(content).byteLength > MAX_CHAT_EXPORT_BYTES)
    return null;
  try {
    const document = parseDocument(content, {
      schema: "core",
      stringKeys: true,
      uniqueKeys: true,
    });
    if (document.errors.length > 0) return null;
    const parsed = record(document.toJS({ maxAliasCount: 0 }));
    const session = parsed ? record(parsed.session) : null;
    if (parsed?.schema !== 1 || session?.platform !== "chatgpt" || !session)
      return null;
    const messages = parseMessages(parsed.messages);
    if (!messages) return null;

    const references = new Map<string, ChatReference>();
    if (Array.isArray(parsed.references)) {
      for (const reference of parsed.references)
        addReference(references, reference, undefined);
    }
    for (const message of messages) {
      for (const referenceUrl of message.references)
        addReference(references, { url: referenceUrl }, message.turn);
    }

    const parsedSession: ChatExport["session"] = {
      platform: "chatgpt",
    };
    const mode = stringValue(session.mode, 100);
    const model = stringValue(session.model, 200);
    const startedAt = stringValue(session.started_at, 100);
    const topic = stringValue(session.topic, 2_000);
    const turnsCount = numberValue(session.turns_count);
    const url = safeUrl(session.url);
    if (mode !== undefined) parsedSession.mode = mode;
    if (model !== undefined) parsedSession.model = model;
    if (startedAt !== undefined) parsedSession.startedAt = startedAt;
    if (topic !== undefined) parsedSession.topic = topic;
    if (turnsCount !== undefined) parsedSession.turnsCount = turnsCount;
    if (url !== undefined) parsedSession.url = url;

    return {
      messages,
      references: [...references.values()],
      session: parsedSession,
    };
  } catch {
    return null;
  }
}
