import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import YAML from "yaml";
import type {
  IngestionArtifact,
  IngestionCallbackStatus,
  IngestionDetailResponse,
  IngestionLiveAction,
  IngestionLiveEvent,
  IngestionLiveJob,
  IngestionListItem,
  IngestionOverviewResponse,
} from "../shared/api";

const safeId = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const maxSnapshotBytes = 20 * 1024 * 1024;
const liveStaleAfterSeconds = 15;

interface IntakeRecord extends Record<string, unknown> {
  intake_id: string;
  source_id: string;
  version_id: string;
  captured_at: string;
  kind: string;
  operation: string;
  requested_outputs: string[];
  status: string | null;
  callback: Record<string, unknown>;
}

interface SourceRecord extends Record<string, unknown> {
  id: string;
  title: string;
  kind: string;
  status?: string | null;
  captured_at?: string | null;
  origin?: Record<string, unknown>;
  versions?: Array<Record<string, unknown>>;
}

interface LiveSnapshot {
  schema: 1;
  generated_at: number;
  job_counts: Record<string, number>;
  jobs: IngestionLiveJob[];
  events: IngestionLiveEvent[];
  actions: IngestionLiveAction[];
}

interface LiveState {
  snapshot?: LiveSnapshot;
  available: boolean;
  stale: boolean;
  generatedAt?: number;
  ageSeconds?: number;
  error?: string;
}

export interface IngestionServiceOptions {
  workspaceRoot: string;
  ingestionRoot?: string;
}

export class IngestionService {
  readonly workspaceRoot: string;
  readonly ingestionRoot: string;

  constructor(options: IngestionServiceOptions) {
    this.workspaceRoot = resolve(options.workspaceRoot);
    this.ingestionRoot = resolve(
      options.ingestionRoot ?? join(this.workspaceRoot, "..", "ingestion"),
    );
  }

  overview(
    searchValue?: string,
    statusValue?: string,
    offset = 0,
    limit = 50,
  ): IngestionOverviewResponse {
    const live = this.readLiveState();
    const records = this.listRecords();
    const search = searchValue?.trim().toLowerCase() ?? "";
    const status = statusValue?.trim().toLowerCase() ?? "";
    const allItems = records.map((record) =>
      this.listItem(record, live.snapshot),
    );
    const items = allItems.filter((item) => {
      if (status && item.status.toLowerCase() !== status) return false;
      if (!search) return true;
      return [item.intake_id, item.source_id, item.source_title, item.kind]
        .join(" ")
        .toLowerCase()
        .includes(search);
    });
    const counts: Record<string, number> = {};
    for (const item of allItems) {
      counts[item.status] = (counts[item.status] ?? 0) + 1;
    }
    const page = items.slice(offset, offset + limit);
    return {
      schema: 1,
      ...this.liveResponseFields(live),
      counts,
      queue_counts: live.snapshot?.job_counts ?? {},
      intakes: page,
      total: items.length,
      offset,
      limit,
      truncated: offset + page.length < items.length,
    };
  }

  detail(intakeId: string): IngestionDetailResponse {
    if (!safeId.test(intakeId)) throw new Error("invalid ingestion intake id");
    const record = this.readIntake(intakeId);
    if (!record) throw new Error("ingestion intake was not found");
    const live = this.readLiveState();
    const source = this.readSource(record.source_id);
    const item = this.listItem(record, live.snapshot, source);
    return {
      schema: 1,
      ...this.liveResponseFields(live),
      intake: item,
      source: {
        id: source?.id ?? record.source_id,
        title: source?.title ?? item.source_title,
        kind: source?.kind ?? record.kind,
        status: source?.status ?? null,
        captured_at: source?.captured_at ?? null,
        origin: source?.origin ?? {},
        versions: source?.versions ?? [],
      },
      artifacts: source ? this.artifacts(source) : [],
    };
  }

  private listRecords(): IntakeRecord[] {
    const directory = join(this.workspaceRoot, "inbox", "ingestion");
    if (!existsSync(directory)) return [];
    const records: IntakeRecord[] = [];
    for (const name of readdirSync(directory)) {
      if (!name.startsWith("intake_") || !name.endsWith(".md")) continue;
      const record = this.readFrontmatter(join(directory, name));
      if (this.isIntake(record)) records.push(record);
    }
    return records.sort((left, right) =>
      right.captured_at.localeCompare(left.captured_at),
    );
  }

  private readIntake(intakeId: string): IntakeRecord | null {
    const record = this.readFrontmatter(
      join(this.workspaceRoot, "inbox", "ingestion", `${intakeId}.md`),
    );
    return this.isIntake(record) ? record : null;
  }

  private readSource(sourceId: string): SourceRecord | null {
    if (!safeId.test(sourceId)) return null;
    const record = this.readFrontmatter(
      join(this.workspaceRoot, "sources", "records", `${sourceId}.md`),
    );
    return this.isSource(record) ? record : null;
  }

  private listItem(
    record: IntakeRecord,
    snapshot?: LiveSnapshot,
    sourceOverride?: SourceRecord | null,
  ): IngestionListItem {
    const source = sourceOverride ?? this.readSource(record.source_id);
    const job = snapshot?.jobs.find(
      (candidate) => candidate.intake_id === record.intake_id,
    );
    const event = job
      ? snapshot?.events.find((candidate) => candidate.job_id === job.job_id)
      : undefined;
    const action = event
      ? snapshot?.actions.find(
          (candidate) => candidate.event_id === event.event_id,
        )
      : snapshot?.actions.find(
          (candidate) => candidate.intake_id === record.intake_id,
        );
    const callbackRecord = record.callback ?? {};
    const callback: IngestionCallbackStatus = {
      event_id: stringOrNull(callbackRecord.event_id),
      durable_status: stringOrNull(callbackRecord.delivery_status),
      delivery_state: event?.delivery_state ?? null,
      attempts: event?.attempts,
      last_error: event?.last_error ?? null,
      delivered_at: event?.delivered_at ?? null,
      acknowledged_at: event?.acknowledged_at ?? null,
    };
    return {
      intake_id: record.intake_id,
      source_id: record.source_id,
      version_id: record.version_id,
      captured_at: record.captured_at,
      kind: record.kind,
      operation: record.operation,
      requested_outputs: record.requested_outputs,
      status: displayStatus(record.status, job?.state),
      durable_status: record.status,
      source_title: source?.title ?? "Untitled source",
      source_status: source?.status ?? null,
      source_url: stringOrNull(source?.origin?.source_url),
      source_record_path: `sources/records/${record.source_id}.md`,
      intake_record_path: `inbox/ingestion/${record.intake_id}.md`,
      job,
      event,
      action,
      callback,
    };
  }

  private artifacts(source: SourceRecord): IngestionArtifact[] {
    const artifacts: IngestionArtifact[] = [];
    for (const version of source.versions ?? []) {
      const versionId = stringOrNull(version.id);
      if (!versionId || !safeId.test(versionId)) continue;
      const originalPath = stringOrNull(version.original_ref);
      if (originalPath) {
        artifacts.push({
          artifact_id: stringOrUndefined(version.original_artifact_id),
          path: originalPath,
          role: "original",
          warnings: [],
        });
      }
      const extractionRoot = join(
        this.workspaceRoot,
        "sources",
        "artifacts",
        source.id,
        versionId,
        "extractions",
      );
      if (!existsSync(extractionRoot)) continue;
      for (const extractionId of readdirSync(extractionRoot)) {
        if (!safeId.test(extractionId)) continue;
        const extractionDirectory = join(extractionRoot, extractionId);
        if (
          !statSync(extractionDirectory, {
            throwIfNoEntry: false,
          })?.isDirectory()
        )
          continue;
        const manifestPath = join(extractionDirectory, "manifest.json");
        const manifest = this.readJson(manifestPath);
        for (const name of readdirSync(extractionDirectory)) {
          if (name === "manifest.json") continue;
          const path = `sources/artifacts/${source.id}/${versionId}/extractions/${extractionId}/${name}`;
          artifacts.push({
            artifact_id: stringOrUndefined(manifest?.artifact_id),
            path,
            role: stringOrNull(manifest?.role) ?? name,
            manifest_path: existsSync(manifestPath)
              ? `sources/artifacts/${source.id}/${versionId}/extractions/${extractionId}/manifest.json`
              : undefined,
            size_bytes: numberOrUndefined(manifest?.size_bytes),
            complete: booleanOrUndefined(manifest?.complete),
            warnings: arrayOfStrings(manifest?.warnings),
          });
        }
      }
    }
    return artifacts;
  }

  private readLiveState(): LiveState {
    const path = join(this.ingestionRoot, "status.json");
    if (!existsSync(path)) return { available: false, stale: false };
    try {
      if (statSync(path).size > maxSnapshotBytes) {
        return {
          available: false,
          stale: false,
          error: "status_snapshot_too_large",
        };
      }
      const value: unknown = JSON.parse(readFileSync(path, "utf8"));
      if (!isLiveSnapshot(value)) {
        return {
          available: false,
          stale: false,
          error: "invalid_status_snapshot",
        };
      }
      const ageSeconds = Math.max(0, Date.now() / 1000 - value.generated_at);
      return {
        snapshot: value,
        available: true,
        stale: ageSeconds > liveStaleAfterSeconds,
        generatedAt: value.generated_at,
        ageSeconds,
      };
    } catch {
      return {
        available: false,
        stale: false,
        error: "status_snapshot_unavailable",
      };
    }
  }

  private liveResponseFields(live: LiveState) {
    return {
      live_available: live.available,
      live_stale: live.stale,
      ...(live.generatedAt === undefined
        ? {}
        : { live_generated_at: live.generatedAt }),
      ...(live.ageSeconds === undefined
        ? {}
        : { live_age_seconds: live.ageSeconds }),
      ...(live.error === undefined ? {} : { live_error: live.error }),
    };
  }

  private readFrontmatter(path: string): Record<string, unknown> | null {
    try {
      const content = readFileSync(path, "utf8");
      const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
      if (!match?.[1]) return null;
      const value: unknown = YAML.parse(match[1]);
      return isObject(value) ? value : null;
    } catch {
      return null;
    }
  }

  private readJson(path: string): Record<string, unknown> | null {
    try {
      const value: unknown = JSON.parse(readFileSync(path, "utf8"));
      return isObject(value) ? value : null;
    } catch {
      return null;
    }
  }

  private isIntake(
    value: Record<string, unknown> | null,
  ): value is IntakeRecord {
    return (
      value !== null &&
      typeof value.intake_id === "string" &&
      safeId.test(value.intake_id) &&
      typeof value.source_id === "string" &&
      safeId.test(value.source_id) &&
      typeof value.version_id === "string" &&
      safeId.test(value.version_id) &&
      typeof value.captured_at === "string" &&
      typeof value.kind === "string" &&
      typeof value.operation === "string" &&
      Array.isArray(value.requested_outputs) &&
      value.requested_outputs.every((item) => typeof item === "string") &&
      (typeof value.status === "string" || value.status === null) &&
      isObject(value.callback)
    );
  }

  private isSource(
    value: Record<string, unknown> | null,
  ): value is SourceRecord {
    return (
      value !== null &&
      typeof value.id === "string" &&
      typeof value.title === "string" &&
      typeof value.kind === "string"
    );
  }
}

function displayStatus(
  durableStatus: string | null,
  liveState?: string,
): string {
  switch (liveState) {
    case "running":
      return "processing";
    case "completed":
      return "complete";
    case "partial":
      return "partial";
    case "queued":
    case "retryable-failure":
    case "awaiting-unlock":
    case "waiting-for-password":
    case "failed":
      return liveState;
    default:
      return durableStatus ?? "unknown";
  }
}

function isLiveSnapshot(value: unknown): value is LiveSnapshot {
  return (
    isObject(value) &&
    value.schema === 1 &&
    typeof value.generated_at === "number" &&
    Number.isFinite(value.generated_at) &&
    isObject(value.job_counts) &&
    Array.isArray(value.jobs) &&
    Array.isArray(value.events) &&
    Array.isArray(value.actions)
  );
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringOrNull(value: unknown): string | null {
  return typeof value === "string" ? value : null;
}

function stringOrUndefined(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function numberOrUndefined(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value)
    ? value
    : undefined;
}

function booleanOrUndefined(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}

function arrayOfStrings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}
