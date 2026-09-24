import { useMemo, useState } from "react";
import type { ComponentProps, ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type {
  WorkspaceFile,
  WorkspaceGitStatus,
  WorkspaceTreeEntry,
} from "../shared/api";

export type WorkspaceView = "edit" | "split" | "preview" | "info";

interface TreeNode {
  path: string;
  name: string;
  kind: WorkspaceTreeEntry["kind"];
  editable?: boolean;
  children: TreeNode[];
}

function buildTree(entries: WorkspaceTreeEntry[]): TreeNode[] {
  const root: TreeNode = {
    children: [],
    kind: "directory",
    name: "",
    path: "",
  };

  for (const entry of entries) {
    const parts = entry.path.split("/");
    let parent = root;
    let currentPath = "";
    parts.forEach((part, index) => {
      currentPath = currentPath ? `${currentPath}/${part}` : part;
      let node = parent.children.find((child) => child.path === currentPath);
      if (!node) {
        const createdNode: TreeNode = {
          children: [],
          kind: index === parts.length - 1 ? entry.kind : "directory",
          name: part,
          path: currentPath,
        };
        if (entry.kind === "file") createdNode.editable = entry.editable;
        node = createdNode;
        parent.children.push(node);
      }
      parent = node;
    });
  }

  return root.children;
}

function countFiles(nodes: TreeNode[]): number {
  return nodes.reduce(
    (count, node) =>
      count + (node.kind === "file" ? 1 : countFiles(node.children)),
    0,
  );
}

function nodeMatches(node: TreeNode, query: string): boolean {
  if (!query) return true;
  return (
    node.path.toLowerCase().includes(query) ||
    node.children.some((child) => nodeMatches(child, query))
  );
}

function matchedFiles(nodes: TreeNode[], query: string): number {
  return nodes.reduce((count, node) => {
    if (node.kind === "file")
      return count + (node.path.toLowerCase().includes(query) ? 1 : 0);
    return count + matchedFiles(node.children, query);
  }, 0);
}

function StatusBadge({
  children,
  tone = "neutral",
}: {
  children: string;
  tone?: "neutral" | "success" | "warning" | "error";
}) {
  return <span className={`badge badge-${tone} gap-1.5`}>{children}</span>;
}

export function WorkspaceHeader({
  authRequired,
  detailsOpen,
  dirty,
  file,
  git,
  onOpenDetails,
  onOpenFiles,
  onSignOut,
}: {
  authRequired: boolean;
  detailsOpen: boolean;
  dirty: boolean;
  file: WorkspaceFile | null;
  git: WorkspaceGitStatus | null;
  onOpenDetails: () => void;
  onOpenFiles: () => void;
  onSignOut: () => void;
}) {
  const gitText = git?.configured
    ? `${git.branch ?? "Git"}${git.dirty ? " / changes" : " / clean"}`
    : "Version control unavailable";

  return (
    <header className="workspace-header">
      <button
        className="btn btn-ghost btn-sm xl:hidden"
        onClick={onOpenFiles}
        type="button"
      >
        Files
      </button>
      <div className="min-w-0 flex-1">
        <p className="workspace-eyebrow">OPENLIA / WORKSPACE</p>
        <h1 className="workspace-title" title={file?.path}>
          {file?.path.split("/").at(-1) ?? "Workspace"}
        </h1>
        {file?.path?.includes("/") && (
          <p className="workspace-path" title={file.path}>
            {file.path}
          </p>
        )}
      </div>
      <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
        <StatusBadge tone={dirty ? "warning" : "success"}>
          {dirty ? "Unsaved" : file ? "Saved" : "Ready"}
        </StatusBadge>
        <span className="workspace-git-status">
          <StatusBadge>{gitText}</StatusBadge>
        </span>
        <button
          className={`workspace-details-toggle btn btn-ghost btn-sm ${detailsOpen ? "text-primary" : ""}`}
          onClick={onOpenDetails}
          type="button"
        >
          Details
        </button>
        {authRequired && (
          <button
            className="btn btn-ghost btn-sm text-primary"
            onClick={onSignOut}
            type="button"
          >
            Sign out
          </button>
        )}
      </div>
    </header>
  );
}

export function FileNavigator({
  entries,
  filter,
  loading,
  mobileOpen,
  onClose,
  onFilterChange,
  onOpenFile,
  onRetry,
  selectedPath,
  truncated,
  workspaceError,
}: {
  entries: WorkspaceTreeEntry[];
  filter: string;
  loading: boolean;
  mobileOpen: boolean;
  onClose: () => void;
  onFilterChange: (value: string) => void;
  onOpenFile: (path: string) => void;
  onRetry: () => void;
  selectedPath: string | undefined;
  truncated: boolean;
  workspaceError: string;
}) {
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const nodes = useMemo(() => buildTree(entries), [entries]);
  const query = filter.trim().toLowerCase();
  const totalFiles = countFiles(nodes);
  const visibleFiles = query ? matchedFiles(nodes, query) : totalFiles;

  function renderNodes(nodesToRender: TreeNode[], depth = 0): ReactNode {
    return (
      <ul className="grid gap-0.5">
        {nodesToRender.map((node) => {
          if (!nodeMatches(node, query)) return null;
          const isCollapsed = collapsed.has(node.path);
          const isDirectory = node.kind === "directory";
          const forcedOpen = Boolean(query);
          const childrenVisible = isDirectory && (!isCollapsed || forcedOpen);
          const directoryId = `workspace-directory-${node.path.replaceAll("/", "-")}`;
          return (
            <li key={node.path}>
              {isDirectory ? (
                <>
                  <button
                    aria-controls={directoryId}
                    aria-expanded={childrenVisible}
                    className="workspace-tree-row workspace-tree-directory"
                    onClick={() => {
                      setCollapsed((current) => {
                        const next = new Set(current);
                        if (next.has(node.path)) next.delete(node.path);
                        else next.add(node.path);
                        return next;
                      });
                    }}
                    style={{ paddingInlineStart: `${depth * 12 + 8}px` }}
                    type="button"
                  >
                    <span aria-hidden="true" className="workspace-tree-chevron">
                      {childrenVisible ? "▾" : "▸"}
                    </span>
                    <span className="truncate">{node.name}</span>
                  </button>
                  {childrenVisible && (
                    <div id={directoryId}>
                      {renderNodes(node.children, depth + 1)}
                    </div>
                  )}
                </>
              ) : (
                <button
                  aria-current={selectedPath === node.path ? "page" : undefined}
                  className={`workspace-tree-row workspace-tree-file ${selectedPath === node.path ? "workspace-tree-file-selected" : ""}`}
                  onClick={() => onOpenFile(node.path)}
                  style={{ paddingInlineStart: `${depth * 12 + 28}px` }}
                  type="button"
                >
                  <span className="min-w-0 truncate">{node.name}</span>
                  {!node.editable && (
                    <span className="text-[10px]">Read only</span>
                  )}
                </button>
              )}
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <>
      {mobileOpen && (
        <button
          aria-label="Close files"
          className="fixed inset-0 z-30 bg-black/60 xl:hidden"
          onClick={onClose}
          type="button"
        />
      )}
      <aside
        aria-label="Workspace files"
        className={`workspace-navigator ${mobileOpen ? "fixed inset-y-0 left-0 z-40 flex w-[min(88vw,340px)]" : "hidden"} xl:relative xl:flex`}
      >
        <div className="flex items-center justify-between border-b border-base-content/10 px-4 py-3 xl:hidden">
          <h2 className="font-semibold">Files</h2>
          <button
            className="btn btn-ghost btn-sm"
            onClick={onClose}
            type="button"
          >
            Close
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-auto p-4">
          <div className="mb-4 flex items-end justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold">Files</h2>
              <p className="mt-1 text-xs text-base-content/50">
                {query
                  ? `${visibleFiles} matching files`
                  : `${totalFiles} files`}
              </p>
            </div>
            {query && (
              <button
                className="btn btn-ghost btn-xs"
                onClick={() => onFilterChange("")}
                type="button"
              >
                Clear
              </button>
            )}
          </div>
          <label className="sr-only" htmlFor="workspace-file-filter">
            Find files
          </label>
          <input
            className="input input-bordered input-sm w-full bg-base-200/50"
            id="workspace-file-filter"
            onChange={(event) => onFilterChange(event.target.value)}
            placeholder="Search paths"
            type="search"
            value={filter}
          />
          {truncated && (
            <p className="alert alert-warning mt-3 p-3 text-xs">
              Showing the first {entries.length} workspace entries.
            </p>
          )}
          {workspaceError ? (
            <div className="mt-5 rounded-lg border border-error/30 bg-error/10 p-4">
              <p className="text-sm font-semibold">Workspace unavailable</p>
              <p className="mt-1 text-xs text-base-content/60">
                Workspace files could not be loaded.
              </p>
              <button
                className="btn btn-error btn-sm mt-3"
                onClick={onRetry}
                type="button"
              >
                Retry
              </button>
            </div>
          ) : loading ? (
            <p className="mt-5 text-sm text-base-content/50">
              Loading files...
            </p>
          ) : visibleFiles === 0 ? (
            <div className="mt-5 rounded-lg border border-base-content/10 bg-base-200/40 p-4">
              <p className="text-sm font-semibold">
                {query ? `No files match “${filter}”` : "No documents yet"}
              </p>
              <p className="mt-1 text-xs text-base-content/60">
                {query
                  ? "Try a different path or clear the search."
                  : "Choose a document when one is available in this workspace."}
              </p>
            </div>
          ) : (
            <div className="mt-4">{renderNodes(nodes)}</div>
          )}
        </div>
      </aside>
    </>
  );
}

const markdownComponents = {
  a: ({ children, ...props }: ComponentProps<"a">) => (
    <a {...props} rel="noreferrer" target="_blank">
      {children}
    </a>
  ),
  input: ({ checked, ...props }: ComponentProps<"input">) => (
    <input {...props} checked={checked} disabled type="checkbox" />
  ),
};

export function MarkdownPreview({ content }: { content: string }) {
  if (!content)
    return (
      <div className="grid h-full min-h-[22rem] place-items-center p-6 text-center text-sm italic text-base-content/50">
        Preview appears here when you select a document.
      </div>
    );

  return (
    <article aria-label="Markdown preview" className="workspace-markdown">
      <ReactMarkdown
        components={markdownComponents}
        remarkPlugins={[remarkGfm]}
      >
        {content}
      </ReactMarkdown>
    </article>
  );
}

export function DocumentInspector({
  diff,
  draft,
  file,
}: {
  diff: string[];
  draft: string;
  file: WorkspaceFile | null;
}) {
  const dirty = file !== null && file.content !== draft;
  return (
    <aside aria-label="Document details" className="workspace-inspector">
      <div className="mb-5">
        <p className="workspace-eyebrow">DOCUMENT</p>
        <h2 className="mt-1 text-xl font-bold tracking-tight">Details</h2>
      </div>
      {!file ? (
        <p className="text-sm text-base-content/50">
          Select a document to inspect it.
        </p>
      ) : (
        <>
          <dl className="grid gap-3 text-sm">
            <div>
              <dt className="text-xs uppercase tracking-wide text-base-content/50">
                Status
              </dt>
              <dd className="mt-1 font-medium">
                {dirty ? "Unsaved draft" : "Saved"}
              </dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-base-content/50">
                Draft size
              </dt>
              <dd className="mt-1 font-medium">
                {new TextEncoder().encode(draft).byteLength} B
              </dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-base-content/50">
                Saved revision
              </dt>
              <dd
                className="mt-1 break-all font-mono text-xs"
                title={file.revision}
              >
                {file.revision}
              </dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-base-content/50">
                Last modified
              </dt>
              <dd className="mt-1 font-medium">
                {new Date(file.modified_at).toLocaleString()}
              </dd>
            </div>
          </dl>
          <div className="mt-8">
            <div className="flex items-center justify-between gap-2">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-base-content/50">
                Draft changes
              </h3>
              {dirty && (
                <span className="text-xs text-warning">
                  Review before saving
                </span>
              )}
            </div>
            <pre className="workspace-diff mt-2">
              {dirty && diff.length ? diff.join("\n") : "No unsaved changes."}
            </pre>
          </div>
        </>
      )}
    </aside>
  );
}

function ModeButton({
  active,
  children,
  className = "",
  onClick,
  value,
}: {
  active: boolean;
  className?: string;
  children: ReactNode;
  onClick: () => void;
  value: string;
}) {
  return (
    <button
      aria-pressed={active}
      className={`btn btn-xs ${active ? "btn-primary" : "btn-ghost"} ${className}`}
      onClick={onClick}
      type="button"
      value={value}
    >
      {children}
    </button>
  );
}

function EditorPane({
  draft,
  file,
  fileLoading,
  onDraftChange,
}: {
  draft: string;
  file: WorkspaceFile;
  fileLoading: boolean;
  onDraftChange: (value: string) => void;
}) {
  return (
    <section aria-label="Editor" className="workspace-editor-pane">
      <div className="workspace-pane-label">Editor</div>
      <textarea
        aria-label="Document editor"
        className="workspace-editor"
        disabled={!file.editable || fileLoading}
        onChange={(event) => onDraftChange(event.target.value)}
        spellCheck={false}
        value={draft}
      />
    </section>
  );
}

export function DocumentPane({
  conflict,
  diff,
  draft,
  documentError,
  file,
  fileLoading,
  onCloseFiles,
  onDownload,
  onDraftChange,
  onOpenDetails,
  onRetry,
  onSave,
  saving,
  view,
  onViewChange,
}: {
  conflict: string;
  diff: string[];
  draft: string;
  documentError: string;
  file: WorkspaceFile | null;
  fileLoading: boolean;
  onCloseFiles: () => void;
  onDownload: () => void;
  onDraftChange: (value: string) => void;
  onOpenDetails: () => void;
  onRetry: () => void;
  onSave: () => void;
  saving: boolean;
  view: WorkspaceView;
  onViewChange: (view: WorkspaceView) => void;
}) {
  const dirty = file !== null && file.content !== draft;
  const canEdit = Boolean(file?.editable);

  return (
    <section aria-label="Document workspace" className="workspace-document">
      {file ? (
        <>
          <div className="workspace-document-toolbar">
            <div className="min-w-0 flex-1">
              <p className="workspace-document-name" title={file.path}>
                {file.path}
              </p>
              {!canEdit && <p className="text-xs text-warning">Read only</p>}
            </div>
            <div aria-label="Document view" className="join" role="toolbar">
              <ModeButton
                active={view === "edit"}
                onClick={() => onViewChange("edit")}
                value="edit"
              >
                Edit
              </ModeButton>
              <ModeButton
                active={view === "split"}
                className="hidden md:inline-flex"
                onClick={() => onViewChange("split")}
                value="split"
              >
                <span className="hidden md:inline">Split</span>
                <span className="md:hidden">Edit</span>
              </ModeButton>
              <ModeButton
                active={view === "preview"}
                onClick={() => onViewChange("preview")}
                value="preview"
              >
                Preview
              </ModeButton>
              <ModeButton
                active={view === "info"}
                className="xl:hidden"
                onClick={onOpenDetails}
                value="info"
              >
                Info
              </ModeButton>
            </div>
            <div className="flex shrink-0 gap-2">
              <button
                className="btn btn-primary btn-sm"
                disabled={!canEdit || !dirty || saving}
                onClick={onSave}
                type="button"
              >
                {saving ? "Saving..." : dirty ? "Save" : "Saved"}
              </button>
              <button
                className="btn btn-outline btn-sm"
                onClick={onDownload}
                type="button"
              >
                Download
              </button>
            </div>
          </div>
          {fileLoading && (
            <div className="workspace-inline-status" role="status">
              Loading document...
            </div>
          )}
          {conflict && (
            <div className="alert alert-warning rounded-none" role="alert">
              <div>
                <p className="font-semibold">
                  This file changed after you opened it.
                </p>
                <p className="text-sm">
                  Your draft is safe. Review it before saving again.
                </p>
              </div>
            </div>
          )}
          {documentError && (
            <div className="alert alert-error rounded-none" role="alert">
              <span>{documentError}</span>
              <button
                className="btn btn-error btn-sm"
                onClick={onRetry}
                type="button"
              >
                Retry
              </button>
            </div>
          )}
          {view === "info" ? (
            <DocumentInspector diff={diff} draft={draft} file={file} />
          ) : view === "edit" ? (
            <EditorPane
              draft={draft}
              file={file}
              fileLoading={fileLoading}
              onDraftChange={onDraftChange}
            />
          ) : view === "preview" ? (
            <section aria-label="Preview" className="workspace-preview-pane">
              <div className="workspace-pane-label">Preview</div>
              <MarkdownPreview content={draft} />
            </section>
          ) : (
            <div className="workspace-split-pane">
              <EditorPane
                draft={draft}
                file={file}
                fileLoading={fileLoading}
                onDraftChange={onDraftChange}
              />
              <section aria-label="Preview" className="workspace-preview-pane">
                <div className="workspace-pane-label">Preview</div>
                <MarkdownPreview content={draft} />
              </section>
            </div>
          )}
        </>
      ) : (
        <div className="workspace-empty-document">
          {documentError ? (
            <div className="max-w-md w-full p-4">
              <div className="alert alert-error rounded-lg" role="alert">
                <span>{documentError}</span>
                <button
                  className="btn btn-error btn-sm"
                  onClick={onRetry}
                  type="button"
                >
                  Retry
                </button>
              </div>
            </div>
          ) : (
            <div className="max-w-sm">
              <p className="workspace-eyebrow">READY WHEN YOU ARE</p>
              <h2 className="mt-2 text-2xl font-bold tracking-tight">
                Choose a document to begin
              </h2>
              <p className="mt-3 text-sm leading-6 text-base-content/60">
                Browse the workspace files, then edit and preview a document
                side by side.
              </p>
              <button
                className="btn btn-primary mt-6 xl:hidden"
                onClick={onCloseFiles}
                type="button"
              >
                Browse files
              </button>
            </div>
          )}
        </div>
      )}
    </section>
  );
}

export function DirtyDraftDialog({
  onCancel,
  onDiscard,
  onSave,
  actionLabel,
  saving,
}: {
  onCancel: () => void;
  onDiscard: () => void;
  onSave: () => void;
  actionLabel: string;
  saving: boolean;
}) {
  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/70 p-4"
      role="presentation"
    >
      <section
        aria-labelledby="unsaved-dialog-title"
        aria-modal="true"
        className="card w-full max-w-md border border-base-content/10 bg-base-100 shadow-2xl"
        role="dialog"
      >
        <div className="card-body">
          <h2 className="card-title" id="unsaved-dialog-title">
            Keep your draft?
          </h2>
          <p className="text-sm leading-6 text-base-content/60">
            You have changes that are not saved. Save them before{" "}
            {actionLabel.toLowerCase()}, discard them, or keep editing.
          </p>
          <div className="card-actions mt-4 justify-end">
            <button className="btn btn-ghost" onClick={onCancel} type="button">
              Keep editing
            </button>
            <button
              className="btn btn-error btn-outline"
              onClick={onDiscard}
              type="button"
            >
              Discard
            </button>
            <button
              className="btn btn-primary"
              disabled={saving}
              onClick={onSave}
              type="button"
            >
              {saving ? "Saving..." : "Save and continue"}
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}
