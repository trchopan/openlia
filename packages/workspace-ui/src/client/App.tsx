import { useEffect, useId, useRef, useState } from "react";
import {
  ApiError,
  downloadUrl,
  loadFile,
  loadGitStatus,
  loadSession,
  loadTree,
  login as loginSession,
  logout as logoutSession,
  saveFile,
} from "./api";
import { diffLines, markdownBlocks, type MarkdownBlock } from "./markdown";
import type {
  WorkspaceFile,
  WorkspaceGitStatus,
  WorkspaceTreeEntry,
} from "../shared/api";
import "./styles.css";

function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function InlineText({ text }: { text: string }) {
  const parts = text.split(/(\*\*[^*]+\*\*|\*[^*]+\*)/g);
  return (
    <>
      {parts.map((part) => {
        if (part.startsWith("**") && part.endsWith("**"))
          return <strong key={`${part}:strong`}>{part.slice(2, -2)}</strong>;
        if (part.startsWith("*") && part.endsWith("*"))
          return <em key={`${part}:em`}>{part.slice(1, -1)}</em>;
        return <span key={`${part}:text`}>{part}</span>;
      })}
    </>
  );
}

function MarkdownPreview({ content }: { content: string }) {
  return (
    <article className="min-w-0 overflow-auto p-6 leading-relaxed text-base-content">
      {markdownBlocks(content).map((block: MarkdownBlock, index) => {
        const key = `${block.kind}:${index}`;
        if (block.kind === "code")
          return (
            <pre
              className="my-4 overflow-auto rounded-lg bg-base-200 p-3 font-mono text-xs leading-normal whitespace-pre-wrap"
              key={key}
            >
              <code>{block.text}</code>
            </pre>
          );
        if (block.kind === "heading") {
          const Heading = `h${block.level}` as "h1" | "h2" | "h3";
          return (
            <Heading
              className={
                block.level === 1
                  ? "mb-4 text-3xl font-bold"
                  : block.level === 2
                    ? "mb-3 text-2xl font-bold"
                    : "mb-2 text-lg font-bold"
              }
              key={key}
            >
              <InlineText text={block.text} />
            </Heading>
          );
        }
        if (block.kind === "blockquote")
          return (
            <blockquote
              className="my-4 border-l-4 border-primary pl-4 text-base-content/60"
              key={key}
            >
              <InlineText text={block.text} />
            </blockquote>
          );
        if (block.kind === "list")
          return (
            <p className="my-1 before:mr-2 before:content-['-']" key={key}>
              <InlineText text={block.text} />
            </p>
          );
        return (
          <p key={key}>
            <InlineText text={block.text} />
          </p>
        );
      })}
      {!content && (
        <p className="text-sm italic text-base-content/50">
          Markdown preview appears here.
        </p>
      )}
    </article>
  );
}

function ErrorMessage({ error }: { error: string }) {
  return (
    <div className="alert alert-error mb-4 rounded-lg" role="alert">
      {error}
    </div>
  );
}

function LoginScreen({
  error,
  onSubmit,
  password,
  setPassword,
}: {
  error: string;
  onSubmit: () => void;
  password: string;
  setPassword: (value: string) => void;
}) {
  const passwordId = useId();
  return (
    <main className="min-h-screen bg-base-300 p-4 text-base-content sm:p-8">
      <section className="card mx-auto flex min-h-[calc(100vh-2rem)] w-full max-w-md justify-center border border-base-content/10 bg-base-100 shadow-2xl sm:min-h-0">
        <div className="card-body p-6 sm:p-8">
          <p className="text-xs font-bold tracking-[0.15em] text-primary">
            OPENLIA / WORKSPACE
          </p>
          <h1 className="mt-2 text-4xl font-bold tracking-tight sm:text-5xl">
            Sign in to the workspace
          </h1>
          <p className="text-sm text-base-content/60">
            Password authentication is enabled for this workspace.
          </p>
          <div className="alert alert-warning mt-4 text-sm">
            This connection is plain HTTP. Use a trusted private network or an
            HTTPS reverse proxy.
          </div>
          <form
            className="mt-6 grid gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              onSubmit();
            }}
          >
            <label className="text-sm font-semibold" htmlFor={passwordId}>
              Password
            </label>
            <input
              autoComplete="current-password"
              className="input input-bordered w-full bg-base-200"
              id={passwordId}
              onChange={(event) => setPassword(event.target.value)}
              type="password"
              value={password}
            />
            <button className="btn btn-primary mt-2 w-full" type="submit">
              Sign in
            </button>
          </form>
          {error && <ErrorMessage error={error} />}
        </div>
      </section>
    </main>
  );
}

export function App() {
  const filterId = useId();
  const [tree, setTree] = useState<WorkspaceTreeEntry[]>([]);
  const [filter, setFilter] = useState("");
  const [file, setFile] = useState<WorkspaceFile | null>(null);
  const [draft, setDraft] = useState("");
  const [git, setGit] = useState<WorkspaceGitStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [fileLoading, setFileLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState("");
  const [authReady, setAuthReady] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [needsLogin, setNeedsLogin] = useState(false);
  const [password, setPassword] = useState("");
  const [authError, setAuthError] = useState("");
  const fileRequestSequence = useRef(0);

  useEffect(() => {
    let active = true;
    void loadSession()
      .then((session) => {
        if (!active) return;
        setAuthRequired(session.auth_required);
        if (session.auth_required && !session.authenticated) {
          setNeedsLogin(true);
          setAuthReady(true);
          setLoading(false);
          return;
        }
        return Promise.all([loadTree(), loadGitStatus()]).then(
          ([treeResponse, gitResponse]) => {
            if (!active) return;
            setTree(treeResponse.entries);
            setGit(gitResponse);
            setAuthReady(true);
          },
        );
      })
      .catch(() => {
        if (active) {
          setError("Workspace data could not be loaded.");
          setAuthReady(true);
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  function handleUnauthorized() {
    setTree([]);
    setFile(null);
    setDraft("");
    setGit(null);
    setNeedsLogin(true);
    setAuthRequired(true);
    setAuthError("Your session has expired. Sign in again.");
  }

  async function submitLogin() {
    setAuthError("");
    try {
      await loginSession(password);
      setPassword("");
      setNeedsLogin(false);
      setLoading(true);
      const [treeResponse, gitResponse] = await Promise.all([
        loadTree(),
        loadGitStatus(),
      ]);
      setTree(treeResponse.entries);
      setGit(gitResponse);
      setAuthReady(true);
      setLoading(false);
    } catch (caught) {
      setPassword("");
      if (caught instanceof ApiError) {
        if (caught.status === 429)
          setAuthError("Too many attempts. Try again shortly.");
        else if (caught.payload?.error === "origin_not_allowed")
          setAuthError(
            "This workspace address is not trusted by the server. Check the reverse proxy origin configuration.",
          );
        else if (caught.status === 401)
          setAuthError("The password was not accepted.");
        else setAuthError("Sign-in failed. Try again shortly.");
      } else setAuthError("Sign-in failed. Try again shortly.");
      setLoading(false);
    }
  }

  async function signOut() {
    await logoutSession().catch(() => undefined);
    handleUnauthorized();
    setAuthError("");
  }

  async function openFile(path: string) {
    const requestSequence = ++fileRequestSequence.current;
    setFileLoading(true);
    setError("");
    setConflict("");
    try {
      const response = await loadFile(path);
      if (requestSequence !== fileRequestSequence.current) return;
      setFile(response);
      setDraft(response.content);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401)
        handleUnauthorized();
      setError("The document could not be opened.");
    } finally {
      if (requestSequence === fileRequestSequence.current)
        setFileLoading(false);
    }
  }

  async function save() {
    if (!file || !file.editable || saving) return;
    setSaving(true);
    setError("");
    setConflict("");
    try {
      const response = await saveFile(file.path, draft, file.revision);
      setFile({ ...file, ...response, content: draft });
      const treeResponse = await loadTree();
      setTree(treeResponse.entries);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) {
        handleUnauthorized();
        setSaving(false);
        return;
      }
      if (caught instanceof ApiError && caught.status === 409) {
        setConflict(
          `Revision conflict. The file changed elsewhere${caught.payload?.current_revision ? ` (${caught.payload.current_revision})` : ""}. Your draft was kept.`,
        );
      } else {
        setError("The document could not be saved.");
      }
    } finally {
      setSaving(false);
    }
  }

  const visibleTree = tree.filter((entry) =>
    entry.path.toLowerCase().includes(filter.toLowerCase()),
  );
  const dirty = file !== null && file.content !== draft;
  const statusText = git?.configured
    ? `${git.branch ?? "Git"}${git.dirty ? " / changes" : " / clean"}`
    : "Git not configured";
  const diff = file ? diffLines(file.content, draft) : [];

  if (!authReady)
    return (
      <main className="grid min-h-screen place-items-center bg-base-300 p-4 text-base-content">
        <p className="flex items-center gap-3 text-sm text-base-content/60">
          <span className="loading loading-spinner loading-sm text-primary" />
          Loading workspace...
        </p>
      </main>
    );
  if (authRequired && needsLogin)
    return (
      <LoginScreen
        error={authError}
        onSubmit={() => void submitLogin()}
        password={password}
        setPassword={setPassword}
      />
    );

  return (
    <main className="mx-auto min-h-screen max-w-[1600px] bg-base-300 p-4 text-base-content sm:p-7">
      <header className="mb-5 flex flex-col items-start justify-between gap-4 lg:flex-row lg:items-end">
        <div>
          <p className="text-xs font-bold tracking-[0.15em] text-primary">
            OPENLIA / WORKSPACE
          </p>
          <h1 className="mt-1 text-4xl font-bold tracking-tight sm:text-6xl">
            Document workbench
          </h1>
        </div>
        <div
          className="flex flex-wrap items-center gap-3 text-sm text-base-content/60"
          aria-live="polite"
        >
          <span className="badge badge-neutral">
            {git ? statusText : "Git status loading..."}
          </span>
          {authRequired && (
            <button
              className="btn btn-ghost btn-sm text-primary"
              onClick={() => void signOut()}
              type="button"
            >
              Sign out
            </button>
          )}
        </div>
      </header>
      {error && <ErrorMessage error={error} />}
      <section className="grid min-h-[calc(100vh-170px)] grid-cols-[210px_minmax(0,1fr)] overflow-hidden rounded-xl border border-base-content/10 bg-base-100 shadow-2xl min-[1051px]:grid-cols-[240px_minmax(0,1fr)_230px] max-[700px]:block">
        <aside className="min-w-0 border-r border-base-content/10 p-4 max-[700px]:border-r-0 max-[700px]:border-b">
          <label
            className="mb-2 block text-xs font-bold text-base-content/70"
            htmlFor={filterId}
          >
            Find files
          </label>
          <input
            className="input input-bordered input-sm w-full bg-base-200/50"
            id={filterId}
            type="search"
            placeholder="Filter by path"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
          <div
            className="mt-4 max-h-[calc(100vh-245px)] overflow-auto max-[700px]:max-h-[220px]"
            aria-live="polite"
          >
            {loading && (
              <p className="text-sm text-base-content/50">
                Loading workspace...
              </p>
            )}
            {!loading && visibleTree.length === 0 && (
              <p className="text-sm text-base-content/50">No matching files.</p>
            )}
            {visibleTree.map((entry) =>
              entry.kind === "directory" ? (
                <div
                  className="block w-full px-2 py-2 text-left text-xs font-bold text-base-content/60"
                  key={entry.path}
                >
                  + {entry.path}
                </div>
              ) : (
                <button
                  className={`btn btn-ghost btn-sm flex h-auto min-h-0 w-full justify-start whitespace-normal px-2 py-2 text-left font-normal normal-case ${entry.path === file?.path ? "bg-primary/15 text-primary hover:bg-primary/20" : ""}`}
                  key={entry.path}
                  onClick={() => void openFile(entry.path)}
                  type="button"
                >
                  {entry.path}
                </button>
              ),
            )}
          </div>
        </aside>
        <section className="min-w-0">
          <div className="flex min-w-0 flex-wrap items-center gap-2 border-b border-base-content/10 p-3 sm:p-4">
            <span
              className="mr-auto min-w-0 flex-1 truncate font-semibold"
              title={file?.path}
            >
              {file?.path ?? "Select a document"}
            </span>
            <button
              className="btn btn-primary btn-sm"
              disabled={!file || !file.editable || !dirty || saving}
              onClick={() => void save()}
              type="button"
            >
              {saving ? "Saving..." : "Save"}
            </button>
            <button
              className="btn btn-outline btn-sm"
              disabled={!file}
              onClick={() => {
                if (file) window.location.href = downloadUrl(file.path);
              }}
              type="button"
            >
              Download
            </button>
          </div>
          {fileLoading && (
            <output className="alert alert-info rounded-none border-x-0 border-t-0 py-2 text-sm">
              Loading document...
            </output>
          )}
          {conflict && (
            <div
              className="alert alert-warning rounded-none border-x-0 border-t-0 py-2 text-sm"
              role="alert"
            >
              {conflict}
            </div>
          )}
          <div className="grid min-h-[calc(100vh-245px)] grid-cols-2 max-[700px]:block max-[700px]:min-h-0">
            <textarea
              aria-label="Document editor"
              className="textarea textarea-ghost h-full min-h-[500px] w-full resize-none rounded-none border-0 border-r border-base-content/10 bg-base-100 p-6 font-mono text-[15px] leading-relaxed text-base-content outline-none focus:border-primary focus:outline-none max-[700px]:min-h-[320px] max-[700px]:border-r-0 max-[700px]:border-b"
              disabled={!file || !file.editable || fileLoading}
              onChange={(event) => setDraft(event.target.value)}
              spellCheck={false}
              value={draft}
            />
            <MarkdownPreview content={file ? draft : ""} />
          </div>
        </section>
        <aside className="border-l border-base-content/10 p-4 max-[1050px]:col-span-2 max-[1050px]:border-l-0 max-[1050px]:border-t max-[700px]:col-auto max-[700px]:border-t-0 max-[700px]:border-b">
          <div>
            <p className="text-xs font-bold tracking-[0.15em] text-primary">
              CONTEXT
            </p>
            <h2 className="mt-1 mb-5 text-xl font-bold tracking-tight">
              Revision-safe editing
            </h2>
          </div>
          <dl className="grid gap-1 text-sm max-[1050px]:grid-cols-3 max-[1050px]:gap-x-5 max-[700px]:grid-cols-2">
            <dt className="text-base-content/60">Revision</dt>
            <dd className="mb-2 break-words">{file?.revision ?? "-"}</dd>
            <dt className="text-base-content/60">Size</dt>
            <dd className="mb-2 break-words">
              {file
                ? formatBytes(new TextEncoder().encode(draft).byteLength)
                : "-"}
            </dd>
            <dt className="text-base-content/60">Modified</dt>
            <dd className="mb-2 break-words">
              {file ? new Date(file.modified_at).toLocaleString() : "-"}
            </dd>
          </dl>
          <div className="mt-7 max-[1050px]:mt-2">
            <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-base-content/60">
              Unsaved changes
            </h3>
            <pre className="min-h-[90px] max-h-80 overflow-auto rounded-lg bg-base-200 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap text-base-content/80">
              {file
                ? diff.length
                  ? diff.join("\n")
                  : "No changes."
                : "No document selected."}
            </pre>
          </div>
        </aside>
      </section>
    </main>
  );
}
