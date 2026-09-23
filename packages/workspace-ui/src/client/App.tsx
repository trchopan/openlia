import { useEffect, useId, useRef, useState } from "react";
import type {
  WorkspaceFile,
  WorkspaceGitStatus,
  WorkspaceTreeEntry,
} from "../shared/api";
import { ApiError, httpWorkspaceApi, type WorkspaceApi } from "./api";
import {
  DirtyDraftDialog,
  DocumentInspector,
  DocumentPane,
  FileNavigator,
  WorkspaceHeader,
  type WorkspaceView,
} from "./components";
import { diffLines } from "./markdown";
import "./styles.css";

type PendingAction =
  | { kind: "open"; path: string }
  | { kind: "signout" }
  | null;

function defaultView(): WorkspaceView {
  if (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(min-width: 768px)").matches
  )
    return "split";
  return "edit";
}

function ErrorMessage({
  error,
  onRetry,
}: {
  error: string;
  onRetry?: () => void;
}) {
  return (
    <div
      className="alert alert-error mb-4 flex-wrap justify-between rounded-lg"
      role="alert"
    >
      <span>{error}</span>
      {onRetry && (
        <button
          className="btn btn-error btn-sm"
          onClick={onRetry}
          type="button"
        >
          Retry
        </button>
      )}
    </div>
  );
}

function LoginScreen({
  error,
  onSubmit,
  password,
  setPassword,
  submitting,
}: {
  error: string;
  onSubmit: () => void;
  password: string;
  setPassword: (value: string) => void;
  submitting: boolean;
}) {
  const passwordId = useId();
  const insecure =
    typeof window !== "undefined" && window.location.protocol !== "https:";

  return (
    <main className="grid min-h-dvh place-items-center bg-base-300 p-4 text-base-content sm:p-8">
      <section className="card w-full max-w-md border border-base-content/10 bg-base-100 shadow-2xl">
        <div className="card-body p-6 sm:p-8">
          <p className="workspace-eyebrow">OPENLIA / WORKSPACE</p>
          <h1 className="mt-2 text-3xl font-bold tracking-tight sm:text-4xl">
            Sign in to your workspace
          </h1>
          <p className="mt-2 text-sm text-base-content/60">
            Enter the workspace password to continue.
          </p>
          {insecure && (
            <div className="alert alert-warning mt-5 text-sm">
              This connection is not encrypted. Use a trusted private network or
              an HTTPS reverse proxy.
            </div>
          )}
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
              disabled={submitting}
              id={passwordId}
              onChange={(event) => setPassword(event.target.value)}
              type="password"
              value={password}
            />
            <button
              className="btn btn-primary mt-3 w-full"
              disabled={submitting || !password}
              type="submit"
            >
              {submitting ? "Signing in..." : "Sign in"}
            </button>
          </form>
          {error && <ErrorMessage error={error} />}
        </div>
      </section>
    </main>
  );
}

export function App({ api = httpWorkspaceApi }: { api?: WorkspaceApi } = {}) {
  const [tree, setTree] = useState<WorkspaceTreeEntry[]>([]);
  const [treeTruncated, setTreeTruncated] = useState(false);
  const [filter, setFilter] = useState("");
  const [file, setFile] = useState<WorkspaceFile | null>(null);
  const [draft, setDraft] = useState("");
  const [git, setGit] = useState<WorkspaceGitStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [fileLoading, setFileLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [workspaceError, setWorkspaceError] = useState("");
  const [documentError, setDocumentError] = useState("");
  const [conflict, setConflict] = useState("");
  const [authReady, setAuthReady] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [needsLogin, setNeedsLogin] = useState(false);
  const [password, setPassword] = useState("");
  const [authError, setAuthError] = useState("");
  const [authSubmitting, setAuthSubmitting] = useState(false);
  const [filesOpen, setFilesOpen] = useState(false);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [view, setView] = useState<WorkspaceView>(defaultView);
  const [pendingAction, setPendingAction] = useState<PendingAction>(null);
  const [reloadToken, setReloadToken] = useState(0);
  const fileRequestSequence = useRef(0);

  const dirty = file !== null && file.content !== draft;
  const diff = dirty ? diffLines(file.content, draft) : [];

  // Retry intentionally reruns this effect even though it does not affect the request payload.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reloadToken is the retry trigger
  useEffect(() => {
    let active = true;
    setAuthReady(false);
    setLoading(true);
    setWorkspaceError("");
    void api
      .loadSession()
      .then((session) => {
        if (!active) return;
        setAuthRequired(session.auth_required);
        if (session.auth_required && !session.authenticated) {
          setNeedsLogin(true);
          setAuthReady(true);
          setLoading(false);
          return;
        }
        return Promise.all([api.loadTree(), api.loadGitStatus()]).then(
          ([treeResponse, gitResponse]) => {
            if (!active) return;
            setTree(treeResponse.entries);
            setTreeTruncated(treeResponse.truncated);
            setGit(gitResponse);
            setNeedsLogin(false);
            setAuthReady(true);
          },
        );
      })
      .catch(() => {
        if (!active) return;
        setWorkspaceError("Workspace data could not be loaded.");
        setGit(null);
        setAuthReady(true);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api, reloadToken]);

  useEffect(() => {
    function warnBeforeUnload(event: BeforeUnloadEvent) {
      if (!dirty) return;
      event.preventDefault();
      event.returnValue = "";
    }
    window.addEventListener("beforeunload", warnBeforeUnload);
    return () => window.removeEventListener("beforeunload", warnBeforeUnload);
  }, [dirty]);

  function handleUnauthorized() {
    setTree([]);
    setGit(null);
    setNeedsLogin(true);
    setAuthRequired(true);
    setAuthReady(true);
    setAuthError("Your session expired. Sign in again to keep working.");
  }

  async function submitLogin() {
    setAuthError("");
    setAuthSubmitting(true);
    try {
      await api.login(password);
      setPassword("");
      setNeedsLogin(false);
      setLoading(true);
      const [treeResponse, gitResponse] = await Promise.all([
        api.loadTree(),
        api.loadGitStatus(),
      ]);
      setTree(treeResponse.entries);
      setTreeTruncated(treeResponse.truncated);
      setGit(gitResponse);
      setWorkspaceError("");
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
    } finally {
      setAuthSubmitting(false);
      setLoading(false);
    }
  }

  async function performSignOut() {
    await api.logout().catch(() => undefined);
    setTree([]);
    setGit(null);
    setFile(null);
    setDraft("");
    setNeedsLogin(true);
    setAuthRequired(true);
    setAuthError("");
  }

  function requestSignOut() {
    if (dirty) setPendingAction({ kind: "signout" });
    else void performSignOut();
  }

  async function openFile(path: string) {
    const requestSequence = ++fileRequestSequence.current;
    setFileLoading(true);
    setDocumentError("");
    setConflict("");
    try {
      const response = await api.loadFile(path);
      if (requestSequence !== fileRequestSequence.current) return;
      setFile(response);
      setDraft(response.content);
      setFilesOpen(false);
      setView(defaultView());
    } catch (caught) {
      if (requestSequence !== fileRequestSequence.current) return;
      if (caught instanceof ApiError && caught.status === 401) {
        handleUnauthorized();
        return;
      }
      setDocumentError("This document could not be opened. Try again.");
    } finally {
      if (requestSequence === fileRequestSequence.current)
        setFileLoading(false);
    }
  }

  function requestOpenFile(path: string) {
    if (path === file?.path) {
      setFilesOpen(false);
      return;
    }
    if (dirty) setPendingAction({ kind: "open", path });
    else void openFile(path);
  }

  async function save(): Promise<boolean> {
    if (!file?.editable || saving) return false;
    setSaving(true);
    setDocumentError("");
    setConflict("");
    try {
      const response = await api.saveFile(file.path, draft, file.revision);
      setFile({ ...file, ...response, content: draft });
      const treeResponse = await api.loadTree();
      setTree(treeResponse.entries);
      setTreeTruncated(treeResponse.truncated);
      const nextGit = await api.loadGitStatus().catch(() => null);
      if (nextGit) setGit(nextGit);
      return true;
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) {
        handleUnauthorized();
        return false;
      }
      if (caught instanceof ApiError && caught.status === 409) {
        setConflict(
          "The file changed elsewhere. Your draft is safe; review it before saving again.",
        );
      } else setDocumentError("The document could not be saved. Try again.");
      return false;
    } finally {
      setSaving(false);
    }
  }

  async function saveAndContinue() {
    const action = pendingAction;
    if (!action) return;
    const saved = await save();
    if (!saved) return;
    setPendingAction(null);
    if (action.kind === "open") void openFile(action.path);
    else void performSignOut();
  }

  function discardAndContinue() {
    const action = pendingAction;
    if (!action) return;
    setPendingAction(null);
    setDraft(file?.content ?? "");
    if (action.kind === "open") void openFile(action.path);
    else void performSignOut();
  }

  function handleViewChange(nextView: WorkspaceView) {
    setView(nextView);
    if (nextView === "info") setDetailsOpen(true);
  }

  function openHeaderDetails() {
    const nextOpen = !detailsOpen;
    setDetailsOpen(nextOpen);
    if (
      typeof window !== "undefined" &&
      !window.matchMedia("(min-width: 1280px)").matches
    )
      setView(nextOpen ? "info" : defaultView());
    else if (!nextOpen && view === "info") setView(defaultView());
  }

  function openInfoView() {
    setDetailsOpen(true);
    setView("info");
  }

  if (!authReady)
    return (
      <main className="grid min-h-dvh place-items-center bg-base-300 p-4 text-base-content">
        <p
          className="flex items-center gap-3 text-sm text-base-content/60"
          role="status"
        >
          <span
            aria-hidden="true"
            className="loading loading-spinner loading-sm text-primary"
          />
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
        submitting={authSubmitting}
      />
    );

  return (
    <main className="workspace-app">
      <WorkspaceHeader
        authRequired={authRequired}
        detailsOpen={detailsOpen}
        dirty={dirty}
        file={file}
        git={git}
        onOpenDetails={openHeaderDetails}
        onOpenFiles={() => setFilesOpen(true)}
        onSignOut={requestSignOut}
      />
      {workspaceError && (
        <ErrorMessage
          error={workspaceError}
          onRetry={() => setReloadToken((current) => current + 1)}
        />
      )}
      <div
        className={`workspace-layout ${detailsOpen ? "xl:grid-cols-[clamp(15rem,18vw,18rem)_minmax(0,1fr)_clamp(16rem,20vw,20rem)]" : "xl:grid-cols-[clamp(15rem,18vw,18rem)_minmax(0,1fr)]"}`}
      >
        <FileNavigator
          entries={tree}
          filter={filter}
          loading={loading}
          mobileOpen={filesOpen}
          onClose={() => setFilesOpen(false)}
          onFilterChange={setFilter}
          onOpenFile={requestOpenFile}
          onRetry={() => setReloadToken((current) => current + 1)}
          selectedPath={file?.path}
          truncated={treeTruncated}
          workspaceError={workspaceError}
        />
        <DocumentPane
          conflict={conflict}
          diff={diff}
          draft={draft}
          documentError={documentError}
          file={file}
          fileLoading={fileLoading}
          onCloseFiles={() => setFilesOpen(true)}
          onDownload={() => {
            if (file) window.location.href = api.downloadUrl(file.path);
          }}
          onDraftChange={setDraft}
          onOpenDetails={openInfoView}
          onRetry={() => {
            if (file) void openFile(file.path);
          }}
          onSave={() => void save()}
          onViewChange={handleViewChange}
          saving={saving}
          view={view}
        />
        {detailsOpen && (
          <div className="hidden min-h-0 xl:block">
            <DocumentInspector diff={diff} draft={draft} file={file} />
          </div>
        )}
      </div>
      {pendingAction && (
        <DirtyDraftDialog
          actionLabel={
            pendingAction.kind === "open"
              ? "opening another file"
              : "signing out"
          }
          onCancel={() => setPendingAction(null)}
          onDiscard={discardAndContinue}
          onSave={() => void saveAndContinue()}
          saving={saving}
        />
      )}
    </main>
  );
}
