import { useMemo, useState } from "react";
import type {
  WorkspaceActivityResponse,
  WorkspaceDiagnosticsResponse,
} from "../shared/api";

export interface ActivitySectionProps {
  activity: WorkspaceActivityResponse | null;
  loading: boolean;
  diagnostics?: WorkspaceDiagnosticsResponse | null;
  diagnosticsLoading?: boolean;
  onOpenFile: (path: string) => void;
  onRefresh?: (() => void) | undefined;
}

type ActivityFilter = "all" | "uncommitted" | "commits" | "files";

function formatRelativeTime(dateString: string): string {
  try {
    const date = new Date(dateString);
    if (Number.isNaN(date.getTime())) return dateString;
    const now = Date.now();
    const diffMs = now - date.getTime();
    if (diffMs < 0) return "Just now";
    const diffSec = Math.floor(diffMs / 1000);
    if (diffSec < 60) return "Just now";
    const diffMin = Math.floor(diffSec / 60);
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHours = Math.floor(diffMin / 60);
    if (diffHours < 24) return `${diffHours}h ago`;
    const diffDays = Math.floor(diffHours / 24);
    if (diffDays < 7) return `${diffDays}d ago`;
    return date.toLocaleDateString();
  } catch {
    return dateString;
  }
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function statusBadge(status: string) {
  switch (status) {
    case "added":
      return (
        <span className="badge badge-success badge-xs font-mono">Added</span>
      );
    case "modified":
      return (
        <span className="badge badge-warning badge-xs font-mono">Modified</span>
      );
    case "deleted":
      return (
        <span className="badge badge-error badge-xs font-mono">Deleted</span>
      );
    case "renamed":
      return (
        <span className="badge badge-info badge-xs font-mono">Renamed</span>
      );
    case "untracked":
      return (
        <span className="badge badge-neutral badge-xs font-mono">
          Untracked
        </span>
      );
    default:
      return (
        <span className="badge badge-ghost badge-xs font-mono">{status}</span>
      );
  }
}

export function ActivitySection({
  activity,
  diagnostics,
  diagnosticsLoading: _diagnosticsLoading,
  loading,
  onOpenFile,
  onRefresh,
}: ActivitySectionProps) {
  const [filter, setFilter] = useState<ActivityFilter>("all");
  const [search, setSearch] = useState("");
  const [expandedCommits, setExpandedCommits] = useState<Set<string>>(
    () => new Set(),
  );

  const toggleCommit = (hash: string) => {
    setExpandedCommits((prev) => {
      const next = new Set(prev);
      if (next.has(hash)) next.delete(hash);
      else next.add(hash);
      return next;
    });
  };

  const uncommitted = activity?.uncommitted ?? [];
  const commits = activity?.commits ?? [];
  const recentFiles = activity?.recentFiles ?? [];

  const filteredUncommitted = useMemo(() => {
    if (!search.trim()) return uncommitted;
    const q = search.toLowerCase();
    return uncommitted.filter((u) => u.path.toLowerCase().includes(q));
  }, [uncommitted, search]);

  const filteredCommits = useMemo(() => {
    if (!search.trim()) return commits;
    const q = search.toLowerCase();
    return commits.filter(
      (c) =>
        c.message.toLowerCase().includes(q) ||
        c.author.toLowerCase().includes(q) ||
        c.shortHash.toLowerCase().includes(q) ||
        c.files.some((f) => f.path.toLowerCase().includes(q)),
    );
  }, [commits, search]);

  const filteredRecentFiles = useMemo(() => {
    if (!search.trim()) return recentFiles;
    const q = search.toLowerCase();
    return recentFiles.filter((f) => f.path.toLowerCase().includes(q));
  }, [recentFiles, search]);

  if (loading && !activity) {
    return (
      <div className="flex h-full min-h-[30rem] items-center justify-center p-8">
        <p
          className="flex items-center gap-3 text-sm text-base-content/60"
          role="status"
        >
          <span
            aria-hidden="true"
            className="loading loading-spinner loading-md text-primary"
          />
          Loading workspace activity...
        </p>
      </div>
    );
  }

  const hasAnyActivity =
    uncommitted.length > 0 || commits.length > 0 || recentFiles.length > 0;

  return (
    <div
      className="workspace-activity flex-1 overflow-y-auto bg-base-100 p-4 sm:p-6 lg:p-8"
      data-testid="workspace-activity"
    >
      {/* Header */}
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <p className="workspace-eyebrow">OPENLIA / WORKSPACE ACTIVITY</p>
          <h2 className="mt-1 text-2xl font-bold tracking-tight">
            Recent Changes
          </h2>
          <p className="mt-1 text-xs text-base-content/60">
            Overview of working tree status, Git commits, and recently touched
            workspace documents.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {activity?.gitConfigured && (
            <span
              className={`badge gap-1.5 py-3 ${
                activity.uncommitted.length > 0
                  ? "badge-warning"
                  : "badge-success"
              }`}
            >
              <span className="font-mono text-xs">
                {activity.branch || "main"}
              </span>
              <span className="opacity-75">
                {activity.uncommitted.length > 0
                  ? `(${activity.uncommitted.length} uncommitted)`
                  : "(clean)"}
              </span>
            </span>
          )}
          {onRefresh && (
            <button
              aria-label="Refresh activity"
              className="btn btn-ghost btn-sm btn-square"
              onClick={onRefresh}
              title="Refresh activity"
              type="button"
            >
              <svg
                className="h-4 w-4"
                fill="none"
                stroke="currentColor"
                strokeWidth={2}
                viewBox="0 0 24 24"
              >
                <title>Refresh</title>
                <path
                  d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            </button>
          )}
        </div>
      </div>

      {/* Template & Schema Diagnostics Panel */}
      {diagnostics && (
        <section
          aria-label="Template and schema diagnostics"
          className="mb-6 rounded-xl border border-base-content/15 bg-base-100 p-4 shadow-sm"
        >
          <div className="flex items-center justify-between gap-2 border-b border-base-content/10 pb-3">
            <div className="flex items-center gap-2">
              <span className="text-sm font-semibold">
                Template & Schema Health
              </span>
              <span
                className={`badge badge-sm font-mono ${
                  diagnostics.valid ? "badge-success" : "badge-warning"
                }`}
              >
                {diagnostics.valid
                  ? "All Conforming"
                  : `${diagnostics.issues.length} ${
                      diagnostics.issues.length === 1 ? "issue" : "issues"
                    }`}
              </span>
            </div>
          </div>

          {diagnostics.issues.length > 0 ? (
            <div className="mt-3 space-y-2.5">
              <p className="text-xs text-base-content/70">
                The following files have frontmatter that does not conform to
                their domain template schemas. Click any file to inspect or
                edit:
              </p>
              <ul className="divide-y divide-base-content/5 rounded-lg border border-base-content/10 bg-base-200/40">
                {diagnostics.issues.map((issue) => (
                  <li
                    key={issue.path}
                    className="flex flex-col sm:flex-row sm:items-start justify-between gap-2 p-3 hover:bg-base-200/80 transition-colors"
                  >
                    <div className="min-w-0 flex-1">
                      <button
                        className="font-mono text-xs font-semibold text-primary hover:underline text-left block truncate"
                        onClick={() => onOpenFile(issue.path)}
                        type="button"
                      >
                        {issue.path}
                      </button>
                      {issue.schema_path && (
                        <span className="text-[10px] text-base-content/50 font-mono block">
                          Schema: {issue.schema_path}
                        </span>
                      )}
                      <ul className="mt-1.5 list-disc list-inside text-xs text-warning space-y-0.5">
                        {issue.errors.map((err) => (
                          <li key={err} className="break-words">
                            {err}
                          </li>
                        ))}
                      </ul>
                    </div>
                    <button
                      className="btn btn-xs btn-outline shrink-0 self-start mt-1 sm:mt-0"
                      onClick={() => onOpenFile(issue.path)}
                      type="button"
                    >
                      Open & Fix
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          ) : (
            <div className="mt-2.5 flex items-center gap-2 text-xs text-success">
              <svg
                className="h-4 w-4 shrink-0"
                fill="none"
                stroke="currentColor"
                strokeWidth={2}
                viewBox="0 0 24 24"
              >
                <title>All schemas and templates valid</title>
                <path
                  d="M5 13l4 4L19 7"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
              <span>
                All documents in this workspace conform to their template
                schemas.
              </span>
            </div>
          )}
        </section>
      )}

      {/* Overview Stat Cards */}
      <div className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-3">
        <div className="rounded-xl border border-base-content/10 bg-base-200/40 p-4">
          <span className="text-xs uppercase tracking-wider text-base-content/50">
            Uncommitted
          </span>
          <p className="mt-1 text-2xl font-bold">{uncommitted.length}</p>
          <span className="text-xs text-base-content/60">
            {uncommitted.length === 1
              ? "file with changes"
              : "files with changes"}
          </span>
        </div>

        <div className="rounded-xl border border-base-content/10 bg-base-200/40 p-4">
          <span className="text-xs uppercase tracking-wider text-base-content/50">
            Git Commits
          </span>
          <p className="mt-1 text-2xl font-bold">{commits.length}</p>
          <span className="text-xs text-base-content/60">
            {commits.length === 1 ? "recent commit" : "recent commits"}
          </span>
        </div>

        <div className="col-span-2 sm:col-span-1 rounded-xl border border-base-content/10 bg-base-200/40 p-4">
          <span className="text-xs uppercase tracking-wider text-base-content/50">
            Tracked Documents
          </span>
          <p className="mt-1 text-2xl font-bold">{recentFiles.length}</p>
          <span className="text-xs text-base-content/60">recent documents</span>
        </div>
      </div>

      {/* Toolbar: Filters and Search */}
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="join border border-base-content/15 rounded-lg bg-base-200/60 p-0.5">
          <button
            className={`btn btn-xs join-item ${
              filter === "all"
                ? "btn-primary shadow-xs"
                : "btn-ghost text-base-content/70"
            }`}
            onClick={() => setFilter("all")}
            type="button"
          >
            All Changes
          </button>
          <button
            className={`btn btn-xs join-item ${
              filter === "uncommitted"
                ? "btn-primary shadow-xs"
                : "btn-ghost text-base-content/70"
            }`}
            onClick={() => setFilter("uncommitted")}
            type="button"
          >
            Uncommitted {uncommitted.length > 0 && `(${uncommitted.length})`}
          </button>
          <button
            className={`btn btn-xs join-item ${
              filter === "commits"
                ? "btn-primary shadow-xs"
                : "btn-ghost text-base-content/70"
            }`}
            onClick={() => setFilter("commits")}
            type="button"
          >
            Commits ({commits.length})
          </button>
          <button
            className={`btn btn-xs join-item ${
              filter === "files"
                ? "btn-primary shadow-xs"
                : "btn-ghost text-base-content/70"
            }`}
            onClick={() => setFilter("files")}
            type="button"
          >
            Recent Files ({recentFiles.length})
          </button>
        </div>

        <div className="w-full sm:w-64">
          <input
            aria-label="Filter activity"
            className="input input-bordered input-sm w-full bg-base-200/50"
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search changes or paths..."
            type="search"
            value={search}
          />
        </div>
      </div>

      {!hasAnyActivity ? (
        <div className="rounded-2xl border border-dashed border-base-content/20 bg-base-200/20 p-12 text-center">
          <h3 className="text-lg font-bold">No workspace activity yet</h3>
          <p className="mt-2 text-sm text-base-content/60">
            Start creating or editing documents to see changes and history
            recorded here.
          </p>
        </div>
      ) : (
        <div className="space-y-8">
          {/* Uncommitted changes section */}
          {(filter === "all" || filter === "uncommitted") &&
            filteredUncommitted.length > 0 && (
              <section
                aria-labelledby="uncommitted-heading"
                className="space-y-3"
              >
                <div className="flex items-center justify-between">
                  <h3
                    className="text-sm font-bold uppercase tracking-wider text-warning"
                    id="uncommitted-heading"
                  >
                    Uncommitted Changes ({filteredUncommitted.length})
                  </h3>
                  <span className="text-xs text-base-content/50">
                    Working tree modifications
                  </span>
                </div>

                <div className="overflow-hidden rounded-xl border border-warning/30 bg-warning/5">
                  <ul className="divide-y divide-base-content/10">
                    {filteredUncommitted.map((item) => (
                      <li key={item.path}>
                        <button
                          className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left transition hover:bg-base-200/60"
                          onClick={() => {
                            if (item.status !== "deleted")
                              onOpenFile(item.path);
                          }}
                          type="button"
                        >
                          <div className="flex min-w-0 items-center gap-2.5">
                            {statusBadge(item.status)}
                            <span
                              className={`truncate text-sm font-medium ${item.status === "deleted" ? "line-through opacity-60" : ""}`}
                            >
                              {item.path}
                            </span>
                          </div>
                          {item.status !== "deleted" && (
                            <span className="btn btn-ghost btn-xs shrink-0 text-primary">
                              Open →
                            </span>
                          )}
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              </section>
            )}

          {/* Commits Section */}
          {(filter === "all" || filter === "commits") &&
            filteredCommits.length > 0 && (
              <section aria-labelledby="commits-heading" className="space-y-3">
                <div className="flex items-center justify-between">
                  <h3
                    className="text-sm font-bold uppercase tracking-wider text-base-content/70"
                    id="commits-heading"
                  >
                    Git Commit History ({filteredCommits.length})
                  </h3>
                  <span className="text-xs text-base-content/50">
                    Chronological history
                  </span>
                </div>

                <div className="space-y-3">
                  {filteredCommits.map((commit) => {
                    const isExpanded = expandedCommits.has(commit.hash);
                    return (
                      <div
                        className="rounded-xl border border-base-content/10 bg-base-200/30 p-4 transition hover:border-base-content/20"
                        key={commit.hash}
                      >
                        <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                          <div className="min-w-0 flex-1">
                            <p className="font-semibold text-base leading-snug">
                              {commit.message}
                            </p>
                            <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-base-content/60">
                              <span className="badge badge-neutral badge-xs font-mono">
                                {commit.shortHash}
                              </span>
                              <span>{commit.author}</span>
                              <span>•</span>
                              <time dateTime={commit.timestamp}>
                                {formatRelativeTime(commit.timestamp)}
                              </time>
                              {commit.files.length > 0 && (
                                <>
                                  <span>•</span>
                                  <button
                                    className="text-primary hover:underline"
                                    onClick={() => toggleCommit(commit.hash)}
                                    type="button"
                                  >
                                    {commit.files.length}{" "}
                                    {commit.files.length === 1
                                      ? "file"
                                      : "files"}{" "}
                                    {isExpanded ? "▴" : "▾"}
                                  </button>
                                </>
                              )}
                            </div>
                          </div>
                        </div>

                        {/* Changed files drawer */}
                        {isExpanded && commit.files.length > 0 && (
                          <div className="mt-3 rounded-lg border border-base-content/10 bg-base-100 p-2 text-xs">
                            <ul className="space-y-1">
                              {commit.files.map((file) => (
                                <li
                                  className="flex items-center justify-between gap-2 px-2 py-1 hover:bg-base-200/50 rounded"
                                  key={file.path}
                                >
                                  <div className="flex min-w-0 items-center gap-2">
                                    {statusBadge(file.status)}
                                    <span
                                      className={`truncate ${file.status === "deleted" ? "line-through opacity-60" : ""}`}
                                    >
                                      {file.path}
                                    </span>
                                  </div>
                                  {file.status !== "deleted" && (
                                    <button
                                      className="text-primary hover:underline shrink-0"
                                      onClick={() => onOpenFile(file.path)}
                                      type="button"
                                    >
                                      View
                                    </button>
                                  )}
                                </li>
                              ))}
                            </ul>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              </section>
            )}

          {/* Recently Modified Files */}
          {(filter === "all" || filter === "files") &&
            filteredRecentFiles.length > 0 && (
              <section aria-labelledby="files-heading" className="space-y-3">
                <div className="flex items-center justify-between">
                  <h3
                    className="text-sm font-bold uppercase tracking-wider text-base-content/70"
                    id="files-heading"
                  >
                    Recently Modified Documents ({filteredRecentFiles.length})
                  </h3>
                  <span className="text-xs text-base-content/50">
                    Sorted by last updated
                  </span>
                </div>

                <div className="overflow-hidden rounded-xl border border-base-content/10 bg-base-200/20">
                  <table className="table table-sm w-full">
                    <thead>
                      <tr className="border-b border-base-content/10 text-xs text-base-content/60">
                        <th>Document</th>
                        <th>Last Modified</th>
                        <th className="hidden sm:table-cell">Size</th>
                        <th className="text-right">Action</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filteredRecentFiles.map((file) => (
                        <tr
                          className="hover:bg-base-200/60 cursor-pointer"
                          key={file.path}
                          onClick={() => onOpenFile(file.path)}
                        >
                          <td className="font-medium">
                            <div className="flex items-center gap-2">
                              <svg
                                className="h-4 w-4 opacity-50 shrink-0"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth={1.5}
                                viewBox="0 0 24 24"
                              >
                                <title>Document</title>
                                <path
                                  d="M19.5 14.25v-2.625a3.375 3.375 0 00-3.375-3.375h-1.5A1.125 1.125 0 0113.5 7.125v-1.5a3.375 3.375 0 00-3.375-3.375H8.25m0 12.75h7.5m-7.5 3H12M10.5 2.25H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 00-9-9z"
                                  strokeLinecap="round"
                                  strokeLinejoin="round"
                                />
                              </svg>
                              <span className="truncate" title={file.path}>
                                {file.path}
                              </span>
                            </div>
                          </td>
                          <td className="text-xs text-base-content/60 whitespace-nowrap">
                            {formatRelativeTime(file.modified_at)}
                          </td>
                          <td className="hidden sm:table-cell text-xs text-base-content/60 font-mono whitespace-nowrap">
                            {formatBytes(file.size)}
                          </td>
                          <td className="text-right">
                            <button
                              className="btn btn-ghost btn-xs text-primary"
                              onClick={(e) => {
                                e.stopPropagation();
                                onOpenFile(file.path);
                              }}
                              type="button"
                            >
                              Open
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </section>
            )}
        </div>
      )}
    </div>
  );
}
