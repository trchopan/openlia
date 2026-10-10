import { useMemo, useState } from "react";
import type {
  IngestionDetailResponse,
  IngestionListItem,
  IngestionOverviewResponse,
} from "../shared/api";

export interface IngestionSectionProps {
  overview: IngestionOverviewResponse | null;
  detail: IngestionDetailResponse | null;
  detailLoading: boolean;
  loading: boolean;
  onOpenFile: (path: string) => void;
  onRefresh: () => void;
  onSelect: (intakeId: string | undefined) => void;
  rawUrl: (path: string) => string;
}

function formatTime(value: string | number | null | undefined): string {
  if (value === null || value === undefined) return "Not recorded";
  const date = new Date(typeof value === "number" ? value * 1000 : value);
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
}

function formatRelative(value: string | number): string {
  const date = new Date(typeof value === "number" ? value * 1000 : value);
  if (Number.isNaN(date.getTime())) return String(value);
  const seconds = Math.max(0, Math.floor((Date.now() - date.getTime()) / 1000));
  if (seconds < 60) return "just now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

function statusClass(status: string): string {
  if (["complete", "completed"].includes(status)) return "badge-success";
  if (["failed", "awaiting-unlock", "waiting-for-password"].includes(status))
    return "badge-error";
  if (["partial", "retryable-failure"].includes(status)) return "badge-warning";
  if (["processing", "running", "queued"].includes(status)) return "badge-info";
  return "badge-ghost";
}

function StatusBadge({ status }: { status: string }) {
  return (
    <span className={`badge badge-sm ${statusClass(status)}`}>{status}</span>
  );
}

function Stage({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-base-content/10 bg-base-200/40 p-3">
      <p className="text-[10px] uppercase tracking-wider text-base-content/50">
        {label}
      </p>
      <p className="mt-1 break-words text-sm font-semibold">{value}</p>
    </div>
  );
}

function ItemRow({
  item,
  selected,
  onSelect,
}: {
  item: IngestionListItem;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      className={`w-full border-b border-base-content/10 p-4 text-left transition-colors last:border-b-0 hover:bg-base-200/70 ${selected ? "bg-primary/10" : ""}`}
      onClick={onSelect}
      type="button"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold">{item.source_title}</p>
          <p className="mt-1 truncate font-mono text-[11px] text-base-content/55">
            {item.intake_id}
          </p>
        </div>
        <StatusBadge status={item.status} />
      </div>
      <div className="mt-3 flex items-center justify-between gap-3 text-xs text-base-content/60">
        <span>
          {item.kind} / {item.operation}
        </span>
        <span>{formatRelative(item.captured_at)}</span>
      </div>
      {item.job && (
        <p className="mt-2 text-[11px] text-info">
          Worker: {item.job.state} · attempt {item.job.attempts}
        </p>
      )}
    </button>
  );
}

function DetailPane({
  detail,
  onOpenFile,
  rawUrl,
}: {
  detail: IngestionDetailResponse;
  onOpenFile: (path: string) => void;
  rawUrl: (path: string) => string;
}) {
  const { intake } = detail;
  return (
    <section
      className="min-w-0 flex-1 overflow-y-auto bg-base-100 p-5 sm:p-7"
      aria-label="Ingestion details"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="workspace-eyebrow">INGESTION / DETAIL</p>
          <h3 className="mt-1 truncate text-xl font-bold">
            {intake.source_title}
          </h3>
          <p className="mt-1 break-all font-mono text-xs text-base-content/55">
            {intake.intake_id}
          </p>
        </div>
        <StatusBadge status={intake.status} />
      </div>

      <div className="mt-5 grid gap-3 sm:grid-cols-3">
        <Stage label="Worker" value={intake.job?.state ?? "No live job"} />
        <Stage
          label="Callback"
          value={
            intake.callback.delivery_state ??
            intake.callback.durable_status ??
            "Not emitted"
          }
        />
        <Stage
          label="Follow-up"
          value={intake.action?.status ?? "Not started"}
        />
      </div>

      {intake.job?.error_summary && (
        <div className="alert alert-error mt-5 text-sm" role="alert">
          <span>
            {intake.job.error_code ? `${intake.job.error_code}: ` : ""}
            {intake.job.error_summary}
          </span>
        </div>
      )}
      {intake.callback.last_error && (
        <div className="alert alert-warning mt-3 text-sm" role="alert">
          <span>Callback delivery: {intake.callback.last_error}</span>
        </div>
      )}

      <div className="mt-6 grid gap-5 lg:grid-cols-2">
        <section className="rounded-xl border border-base-content/10 bg-base-200/30 p-4">
          <h4 className="font-semibold">Live execution</h4>
          <dl className="mt-3 space-y-2 text-sm">
            <DetailRow
              label="Job ID"
              value={intake.job?.job_id ?? "Not queued"}
              mono
            />
            <DetailRow
              label="Attempts"
              value={intake.job ? String(intake.job.attempts) : "Not queued"}
            />
            <DetailRow
              label="Heartbeat"
              value={formatTime(intake.job?.heartbeat_at)}
            />
            <DetailRow
              label="Lease expires"
              value={formatTime(intake.job?.lease_expires_at)}
            />
            <DetailRow
              label="Next attempt"
              value={formatTime(intake.job?.next_attempt_at)}
            />
            <DetailRow
              label="Updated"
              value={formatTime(intake.job?.updated_at)}
            />
          </dl>
        </section>
        <section className="rounded-xl border border-base-content/10 bg-base-200/30 p-4">
          <h4 className="font-semibold">Callback and continuation</h4>
          <dl className="mt-3 space-y-2 text-sm">
            <DetailRow
              label="Event ID"
              value={intake.callback.event_id ?? "Not emitted"}
              mono
            />
            <DetailRow
              label="Delivery attempts"
              value={
                intake.callback.attempts === undefined
                  ? "Not emitted"
                  : String(intake.callback.attempts)
              }
            />
            <DetailRow
              label="Delivered"
              value={formatTime(intake.callback.delivered_at)}
            />
            <DetailRow
              label="Acknowledged"
              value={formatTime(intake.callback.acknowledged_at)}
            />
            <DetailRow
              label="Action ID"
              value={intake.action?.action_id ?? "Not started"}
              mono
            />
            <DetailRow
              label="Record refs"
              value={intake.action?.record_refs.join(", ") || "None"}
            />
          </dl>
        </section>
      </div>

      <section className="mt-5 rounded-xl border border-base-content/10 bg-base-200/30 p-4">
        <h4 className="font-semibold">Durable records and artifacts</h4>
        <div className="mt-3 flex flex-wrap gap-2">
          <button
            className="btn btn-sm btn-outline"
            onClick={() => onOpenFile(intake.intake_record_path)}
            type="button"
          >
            Open intake record
          </button>
          <button
            className="btn btn-sm btn-outline"
            onClick={() => onOpenFile(intake.source_record_path)}
            type="button"
          >
            Open source record
          </button>
        </div>
        {detail.artifacts.length > 0 ? (
          <div className="mt-4 divide-y divide-base-content/10 rounded-lg border border-base-content/10">
            {detail.artifacts.map((artifact) => (
              <div
                className="flex flex-col gap-2 p-3 sm:flex-row sm:items-center sm:justify-between"
                key={artifact.path}
              >
                <div className="min-w-0">
                  <p className="text-xs font-semibold">{artifact.role}</p>
                  <p
                    className="truncate font-mono text-[11px] text-base-content/55"
                    title={artifact.path}
                  >
                    {artifact.path}
                  </p>
                  {artifact.warnings.length > 0 && (
                    <p className="mt-1 text-xs text-warning">
                      {artifact.warnings.join(", ")}
                    </p>
                  )}
                </div>
                <a
                  className="btn btn-ghost btn-xs shrink-0 text-primary"
                  href={rawUrl(artifact.path)}
                  rel="noreferrer"
                  target="_blank"
                >
                  Open
                </a>
              </div>
            ))}
          </div>
        ) : (
          <p className="mt-4 text-sm text-base-content/60">
            No published artifacts yet.
          </p>
        )}
      </section>

      {intake.source_url && (
        <a
          className="mt-4 block truncate text-sm text-primary hover:underline"
          href={intake.source_url}
          rel="noreferrer"
          target="_blank"
        >
          {intake.source_url}
        </a>
      )}
      <p className="mt-5 text-xs text-base-content/50">
        Captured {formatTime(intake.captured_at)}. Live snapshot{" "}
        {detail.live_stale ? "is stale" : "updated"}{" "}
        {detail.live_age_seconds === undefined
          ? "unavailable"
          : `${Math.round(detail.live_age_seconds)}s ago`}
        .
      </p>
    </section>
  );
}

function DetailRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-start justify-between gap-3 border-b border-base-content/5 pb-2 last:border-0 last:pb-0">
      <dt className="text-base-content/55">{label}</dt>
      <dd
        className={`max-w-[65%] break-words text-right ${mono ? "font-mono text-xs" : ""}`}
      >
        {value}
      </dd>
    </div>
  );
}

export function IngestionSection({
  overview,
  detail,
  detailLoading,
  loading,
  onOpenFile,
  onRefresh,
  onSelect,
  rawUrl,
}: IngestionSectionProps) {
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const filtered = useMemo(() => {
    const items = overview?.intakes ?? [];
    const query = search.trim().toLowerCase();
    return items.filter((item) => {
      if (status && item.status !== status) return false;
      if (!query) return true;
      return `${item.source_title} ${item.intake_id} ${item.kind}`
        .toLowerCase()
        .includes(query);
    });
  }, [overview?.intakes, search, status]);

  if (loading && !overview) {
    return (
      <div className="flex min-h-[30rem] flex-1 items-center justify-center">
        <span
          className="loading loading-spinner loading-md text-primary"
          role="status"
        />
      </div>
    );
  }

  const counts = overview?.counts ?? {};
  const selectedId = detail?.intake.intake_id;
  return (
    <div
      className="flex min-h-0 flex-1 flex-col overflow-hidden bg-base-100"
      data-testid="ingestion-section"
    >
      <div className="border-b border-base-content/10 bg-base-100 p-5 sm:p-7">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <p className="workspace-eyebrow">OPENLIA / INGESTION RUNTIME</p>
            <h2 className="mt-1 text-2xl font-bold tracking-tight">
              Ingestion Control Room
            </h2>
            <p className="mt-1 text-xs text-base-content/60">
              Live queue execution, Hermes callbacks, and durable extraction
              records.
            </p>
          </div>
          <button
            aria-label="Refresh ingestion"
            className="btn btn-ghost btn-sm btn-square"
            onClick={onRefresh}
            type="button"
          >
            ↻
          </button>
        </div>
        <div className="mt-5 flex flex-wrap items-center gap-2">
          {Object.entries(counts)
            .sort(([a], [b]) => a.localeCompare(b))
            .map(([key, value]) => (
              <button
                className={`badge badge-md cursor-pointer ${status === key ? "badge-primary" : "badge-outline"}`}
                key={key}
                onClick={() => setStatus(status === key ? "" : key)}
                type="button"
              >
                {key}: {value}
              </button>
            ))}
          {overview?.live_available ? (
            <span
              className={`badge badge-md ${overview.live_stale ? "badge-warning" : "badge-success"}`}
            >
              {overview.live_stale ? "Live snapshot stale" : "Live connected"}
            </span>
          ) : (
            <span className="badge badge-md badge-ghost">
              Durable records only
            </span>
          )}
        </div>
        <div className="mt-4 flex flex-col gap-2 sm:flex-row">
          <input
            aria-label="Search ingestion"
            className="input input-bordered input-sm w-full sm:max-w-sm"
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search intake or source..."
            type="search"
            value={search}
          />
          {overview?.live_error && (
            <p className="self-center text-xs text-warning">
              {overview.live_error}
            </p>
          )}
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col xl:flex-row">
        <section
          className="max-h-[28rem] w-full overflow-y-auto border-b border-base-content/10 xl:max-h-none xl:w-[min(34rem,38%)] xl:border-b-0 xl:border-r"
          aria-label="Ingestion intakes"
        >
          {filtered.length > 0 ? (
            filtered.map((item) => (
              <ItemRow
                item={item}
                key={item.intake_id}
                onSelect={() => onSelect(item.intake_id)}
                selected={selectedId === item.intake_id}
              />
            ))
          ) : (
            <div className="p-8 text-center text-sm text-base-content/60">
              No ingestion records match this filter.
            </div>
          )}
        </section>
        {detailLoading ? (
          <div className="flex min-h-[24rem] flex-1 items-center justify-center">
            <span
              className="loading loading-spinner loading-md text-primary"
              role="status"
            />
          </div>
        ) : detail ? (
          <DetailPane detail={detail} onOpenFile={onOpenFile} rawUrl={rawUrl} />
        ) : (
          <div className="flex min-h-[24rem] flex-1 items-center justify-center p-8 text-center text-sm text-base-content/60">
            Select an intake to inspect its live execution and published
            artifacts.
          </div>
        )}
      </div>
    </div>
  );
}
