import { useCallback, useEffect, useId, useRef, useState } from "react";
import type {
  SkillDetail,
  SkillFileEntry,
  SkillFileResponse,
  SkillSummary,
  WorkspaceFile,
  WorkspaceGitStatus,
  WorkspaceTreeEntry,
} from "../shared/api";
import { CreateSkillModal } from "./CreateSkillModal";
import { SkillDetailPane, type SkillSubTab } from "./SkillDetailPane";
import { SkillsNavigator } from "./SkillsNavigator";
import { ApiError, httpWorkspaceApi, type WorkspaceApi } from "./api";
import { isChatgptExportPath } from "./chatgpt";
import {
  DirtyDraftDialog,
  DocumentInspector,
  DocumentPane,
  FileNavigator,
  WorkspaceHeader,
  type WorkspaceView,
} from "./components";
import { diffLines } from "./markdown";
import { type MainTab, navigateRoute, parseRoute } from "./route";
import "./styles.css";

type PendingAction =
  | { kind: "open"; path: string }
  | { kind: "openSkill"; id: string; skillFile?: string | undefined }
  | { kind: "switchTab"; tab: MainTab }
  | { kind: "signout" }
  | null;

function defaultView(): WorkspaceView {
  return "preview";
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
  const initialRoute = useRef(
    typeof window !== "undefined"
      ? parseRoute(window.location)
      : {
          filter: undefined,
          path: undefined,
          scenario: undefined,
          skill: undefined,
          skillFile: undefined,
          tab: undefined,
          view: undefined,
        },
  );

  const [activeTab, setActiveTab] = useState<MainTab>(
    () =>
      initialRoute.current.tab ??
      (initialRoute.current.skill ? "skills" : "documents"),
  );

  // Documents state
  const [tree, setTree] = useState<WorkspaceTreeEntry[]>([]);
  const [treeTruncated, setTreeTruncated] = useState(false);
  const [filter, setFilter] = useState(() => initialRoute.current.filter ?? "");
  const [file, setFile] = useState<WorkspaceFile | null>(null);
  const [draft, setDraft] = useState("");
  const [git, setGit] = useState<WorkspaceGitStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [fileLoading, setFileLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [workspaceError, setWorkspaceError] = useState("");
  const [documentError, setDocumentError] = useState("");
  const [conflict, setConflict] = useState("");

  // Skills state
  const [skills, setSkills] = useState<SkillSummary[]>([]);
  const [skillCategories, setSkillCategories] = useState<string[]>([]);
  const [skillsFilter, setSkillsFilter] = useState("");
  const [selectedSkillId, setSelectedSkillId] = useState<string | null>(
    () => initialRoute.current.skill ?? null,
  );
  const [selectedSkillDetail, setSelectedSkillDetail] =
    useState<SkillDetail | null>(null);
  const [selectedSkillFile, setSelectedSkillFile] =
    useState<SkillFileResponse | null>(null);
  const [activeSkillSubTab, setActiveSkillSubTab] =
    useState<SkillSubTab>("instructions");
  const [skillDraft, setSkillDraft] = useState("");
  const [skillsLoading, setSkillsLoading] = useState(false);
  const [skillFileLoading, setSkillFileLoading] = useState(false);
  const [skillSaving, setSkillSaving] = useState(false);
  const [skillError, setSkillError] = useState("");
  const [isCreateSkillOpen, setIsCreateSkillOpen] = useState(false);

  // Auth & Navigation state
  const [authReady, setAuthReady] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [needsLogin, setNeedsLogin] = useState(false);
  const [password, setPassword] = useState("");
  const [authError, setAuthError] = useState("");
  const [authSubmitting, setAuthSubmitting] = useState(false);
  const [filesOpen, setFilesOpen] = useState(false);
  const filesButtonRef = useRef<HTMLButtonElement>(null);
  const [detailsOpen, setDetailsOpen] = useState(
    () => initialRoute.current.view === "info",
  );
  const [view, setView] = useState<WorkspaceView>(
    () => initialRoute.current.view ?? defaultView(),
  );
  const [pendingAction, setPendingAction] = useState<PendingAction>(null);
  const fileRequestSequence = useRef(0);
  const skillRequestSequence = useRef(0);
  const initialPathOpened = useRef(false);

  const docDirty = file !== null && file.content !== draft;
  const skillDirty =
    selectedSkillFile !== null && selectedSkillFile.content !== skillDraft;
  const dirty = activeTab === "skills" ? skillDirty : docDirty;
  const diff = docDirty && file ? diffLines(file.content, draft) : [];

  const appStateRef = useRef({
    activeTab,
    dirty,
    docDirty,
    file,
    filter,
    openFile,
    openSkill,
    save,
    saveSkillFile,
    selectedSkillFile,
    skillDirty,
    view,
  });
  useEffect(() => {
    appStateRef.current = {
      activeTab,
      dirty,
      docDirty,
      file,
      filter,
      openFile,
      openSkill,
      save,
      saveSkillFile,
      selectedSkillFile,
      skillDirty,
      view,
    };
  });

  const loadWorkspace = useCallback(
    async (isActive?: () => boolean) => {
      setAuthReady(false);
      setLoading(true);
      setWorkspaceError("");
      try {
        const session = await api.loadSession();
        if (isActive && !isActive()) return;
        setAuthRequired(session.auth_required);
        if (session.auth_required && !session.authenticated) {
          setNeedsLogin(true);
          setAuthReady(true);
          setLoading(false);
          return;
        }

        const [treeResponse, gitResponse, skillsResponse] = await Promise.all([
          api.loadTree(),
          api.loadGitStatus().catch(() => null),
          api
            .loadSkills()
            .catch(() => ({ categories: [], schema: 1 as const, skills: [] })),
        ]);
        if (isActive && !isActive()) return;

        setTree(treeResponse.entries);
        setTreeTruncated(treeResponse.truncated);
        setGit(gitResponse);
        setSkills(skillsResponse.skills);
        setSkillCategories(skillsResponse.categories);
        setNeedsLogin(false);
        setAuthReady(true);

        if (!initialPathOpened.current) {
          initialPathOpened.current = true;
          const route =
            typeof window !== "undefined"
              ? parseRoute(window.location)
              : initialRoute.current;

          if (route.tab === "skills" || route.skill) {
            setActiveTab("skills");
            if (route.skill) {
              void appStateRef.current.openSkill(route.skill, route.skillFile);
            }
          } else if (route.path) {
            void appStateRef.current.openFile(route.path, {
              keepView: Boolean(route.view),
              replaceHistory: true,
            });
          }
        }
      } catch {
        if (isActive && !isActive()) return;
        setWorkspaceError("Workspace data could not be loaded.");
        setGit(null);
        setAuthReady(true);
      } finally {
        if (!isActive || isActive()) {
          setLoading(false);
        }
      }
    },
    [api],
  );

  useEffect(() => {
    let active = true;
    void loadWorkspace(() => active);
    return () => {
      active = false;
    };
  }, [loadWorkspace]);

  useEffect(() => {
    function warnBeforeUnload(event: BeforeUnloadEvent) {
      if (!dirty) return;
      event.preventDefault();
      event.returnValue = "";
    }
    window.addEventListener("beforeunload", warnBeforeUnload);
    return () => window.removeEventListener("beforeunload", warnBeforeUnload);
  }, [dirty]);

  useEffect(() => {
    function handleSaveShortcut(event: KeyboardEvent) {
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "s")
        return;
      const { activeTab, docDirty, file, save, saveSkillFile, skillDirty } =
        appStateRef.current;
      if (activeTab === "skills") {
        if (!skillDirty) return;
        event.preventDefault();
        void saveSkillFile();
      } else {
        if (!docDirty || !file?.editable) return;
        event.preventDefault();
        void save();
      }
    }

    window.addEventListener("keydown", handleSaveShortcut);
    return () => window.removeEventListener("keydown", handleSaveShortcut);
  }, []);

  useEffect(() => {
    function handlePopState() {
      const { activeTab, dirty, file, filter, openFile, openSkill, view } =
        appStateRef.current;
      const route = parseRoute(window.location);

      if (dirty) {
        if (activeTab === "documents" && file?.path) {
          navigateRoute(
            {
              filter: filter || undefined,
              path: file.path,
              scenario: route.scenario,
              tab: "documents",
              view,
            },
            { replace: true },
          );
        }
        if (route.tab === "skills" || route.skill) {
          setPendingAction({
            id: route.skill ?? "",
            kind: "openSkill",
            skillFile: route.skillFile,
          });
        } else {
          setPendingAction({ kind: "open", path: route.path ?? "" });
        }
        return;
      }

      setFilter(route.filter ?? "");

      if (route.tab) {
        setActiveTab(route.tab);
      }

      if (route.view) {
        setView(route.view);
        if (route.view === "info") setDetailsOpen(true);
      }

      if (route.tab === "skills" || route.skill) {
        setActiveTab("skills");
        if (route.skill) {
          void openSkill(route.skill, route.skillFile);
        }
      } else if (route.path) {
        if (route.path !== file?.path) {
          void openFile(route.path, {
            keepView: Boolean(route.view),
            replaceHistory: true,
          });
        }
      } else {
        setFile(null);
        setDraft("");
        setConflict("");
        setDocumentError("");
      }
    }

    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  useEffect(() => {
    if (typeof document === "undefined") return;
    if (activeTab === "skills") {
      document.title = selectedSkillDetail
        ? `${selectedSkillDetail.name} - OpenLia Skills`
        : "Skills - OpenLia Workspace";
    } else {
      if (file?.path) {
        const name = file.path.split("/").at(-1) ?? file.path;
        document.title = `${name} - OpenLia Workspace`;
      } else {
        document.title = "OpenLia Workspace";
      }
    }
  }, [activeTab, file?.path, selectedSkillDetail]);

  function handleUnauthorized() {
    setTree([]);
    setSkills([]);
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
      const [treeResponse, gitResponse, skillsResponse] = await Promise.all([
        api.loadTree(),
        api.loadGitStatus().catch(() => null),
        api
          .loadSkills()
          .catch(() => ({ categories: [], schema: 1 as const, skills: [] })),
      ]);
      setTree(treeResponse.entries);
      setTreeTruncated(treeResponse.truncated);
      setGit(gitResponse);
      setSkills(skillsResponse.skills);
      setSkillCategories(skillsResponse.categories);
      setWorkspaceError("");

      if (!initialPathOpened.current) {
        initialPathOpened.current = true;
        const route =
          typeof window !== "undefined"
            ? parseRoute(window.location)
            : initialRoute.current;
        if (route.tab === "skills" || route.skill) {
          setActiveTab("skills");
          if (route.skill) {
            void appStateRef.current.openSkill(route.skill, route.skillFile);
          }
        } else if (route.path) {
          void openFile(route.path, {
            keepView: Boolean(route.view),
            replaceHistory: true,
          });
        }
      }
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
    setSkills([]);
    setGit(null);
    setFile(null);
    setDraft("");
    setSelectedSkillDetail(null);
    setSelectedSkillFile(null);
    setNeedsLogin(true);
    setAuthRequired(true);
    setAuthError("");
    const currentRoute =
      typeof window !== "undefined" ? parseRoute(window.location) : {};
    navigateRoute({ scenario: currentRoute.scenario }, { replace: true });
  }

  function requestSignOut() {
    if (dirty) setPendingAction({ kind: "signout" });
    else void performSignOut();
  }

  function requestTabChange(nextTab: MainTab) {
    if (nextTab === activeTab) return;
    if (dirty) {
      setPendingAction({ kind: "switchTab", tab: nextTab });
    } else {
      setActiveTab(nextTab);
      navigateRoute({
        path: nextTab === "documents" ? file?.path : undefined,
        skill:
          nextTab === "skills" ? (selectedSkillId ?? undefined) : undefined,
        tab: nextTab,
      });
    }
  }

  async function openFile(
    path: string,
    options?: { keepView?: boolean; replaceHistory?: boolean },
  ) {
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
      let nextView = view;
      if (
        isChatgptExportPath(path) &&
        !(options?.keepView && view === "info")
      ) {
        nextView = "preview";
        setView(nextView);
      } else if (!options?.keepView) {
        nextView = defaultView();
        setView(nextView);
      }
      const currentRoute =
        typeof window !== "undefined" ? parseRoute(window.location) : {};
      navigateRoute(
        {
          filter: filter || undefined,
          path,
          scenario: currentRoute.scenario,
          tab: "documents",
          view: nextView,
        },
        { replace: options?.replaceHistory },
      );
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
    else void openFile(path, { keepView: true });
  }

  async function openSkill(id: string, skillFilePath = "SKILL.md") {
    const requestSequence = ++skillRequestSequence.current;
    setSkillsLoading(true);
    setSkillError("");
    try {
      const [detailRes, fileRes] = await Promise.all([
        api.loadSkillDetail(id),
        api.loadSkillFile(id, skillFilePath).catch(() => null),
      ]);
      if (requestSequence !== skillRequestSequence.current) return;
      setSelectedSkillId(id);
      setSelectedSkillDetail(detailRes.skill);
      setSelectedSkillFile(fileRes);
      setSkillDraft(fileRes ? fileRes.content : "");
      setActiveSkillSubTab(
        skillFilePath === "SKILL.md" ? "instructions" : "files",
      );
      setFilesOpen(false);

      navigateRoute({
        skill: id,
        skillFile: skillFilePath !== "SKILL.md" ? skillFilePath : undefined,
        tab: "skills",
      });
    } catch (caught) {
      if (requestSequence !== skillRequestSequence.current) return;
      if (caught instanceof ApiError && caught.status === 401) {
        handleUnauthorized();
        return;
      }
      setSkillError("Failed to load skill details.");
    } finally {
      if (requestSequence === skillRequestSequence.current)
        setSkillsLoading(false);
    }
  }

  function requestOpenSkill(id: string, skillFilePath?: string) {
    if (
      id === selectedSkillId &&
      (!skillFilePath || skillFilePath === selectedSkillFile?.path)
    ) {
      setFilesOpen(false);
      return;
    }
    if (dirty) {
      setPendingAction({ id, kind: "openSkill", skillFile: skillFilePath });
    } else {
      void openSkill(id, skillFilePath);
    }
  }

  async function openSkillFile(fileEntry: SkillFileEntry) {
    if (!selectedSkillId) return;
    if (skillDirty) {
      setPendingAction({
        id: selectedSkillId,
        kind: "openSkill",
        skillFile: fileEntry.path,
      });
      return;
    }
    setSkillFileLoading(true);
    try {
      const res = await api.loadSkillFile(selectedSkillId, fileEntry.path);
      setSelectedSkillFile(res);
      setSkillDraft(res.content);
    } catch {
      setSkillError(`Could not load ${fileEntry.path}`);
    } finally {
      setSkillFileLoading(false);
    }
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

  async function saveSkillFile(): Promise<boolean> {
    if (!selectedSkillDetail || !selectedSkillFile || skillSaving) return false;
    setSkillSaving(true);
    setSkillError("");
    try {
      const res = await api.saveSkillFile(
        selectedSkillDetail.id,
        selectedSkillFile.path,
        skillDraft,
        selectedSkillFile.revision,
      );
      setSelectedSkillFile({
        ...selectedSkillFile,
        content: skillDraft,
        modified_at: res.modified_at,
        revision: res.revision,
      });

      // If SKILL.md was updated, reload detail and skills list to refresh frontmatter description
      if (selectedSkillFile.path === "SKILL.md") {
        const [updatedDetail, updatedSkills] = await Promise.all([
          api.loadSkillDetail(selectedSkillDetail.id),
          api.loadSkills(),
        ]);
        setSelectedSkillDetail(updatedDetail.skill);
        setSkills(updatedSkills.skills);
      }
      return true;
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 409) {
        setSkillError(
          "Revision conflict. The file changed on disk. Revert or copy your changes.",
        );
      } else {
        setSkillError("Failed to save skill file.");
      }
      return false;
    } finally {
      setSkillSaving(false);
    }
  }

  async function handleToggleSkillEnable(enabled: boolean) {
    if (!selectedSkillDetail) return;
    try {
      await api.toggleSkillEnable(selectedSkillDetail.id, enabled);
      setSelectedSkillDetail({ ...selectedSkillDetail, enabled });
      setSkills((prev) =>
        prev.map((s) =>
          s.id === selectedSkillDetail.id ? { ...s, enabled } : s,
        ),
      );
    } catch {
      setSkillError("Failed to toggle skill enable state.");
    }
  }

  async function handleToggleSkillPin(id: string, pinned: boolean) {
    try {
      await api.toggleSkillPin(id, pinned);
      if (selectedSkillDetail?.id === id) {
        setSelectedSkillDetail({ ...selectedSkillDetail, pinned });
      }
      setSkills((prev) =>
        prev.map((s) => (s.id === id ? { ...s, pinned } : s)),
      );
    } catch {
      setSkillError("Failed to update skill pin state.");
    }
  }

  async function handleCreateSkill(
    name: string,
    category: string,
    description: string,
  ) {
    const res = await api.createSkill({ category, description, name });
    const skillsRes = await api.loadSkills();
    setSkills(skillsRes.skills);
    setSkillCategories(skillsRes.categories);
    if (res.id) {
      void openSkill(res.id);
    }
  }

  async function saveAndContinue() {
    const action = pendingAction;
    if (!action) return;
    const ok = activeTab === "skills" ? await saveSkillFile() : await save();
    if (!ok) return;
    setPendingAction(null);

    if (action.kind === "open") {
      if (action.path) {
        void openFile(action.path, { keepView: true });
      } else {
        setFile(null);
        setDraft("");
        navigateRoute({ tab: "documents", view }, { replace: true });
      }
    } else if (action.kind === "openSkill") {
      void openSkill(action.id, action.skillFile);
    } else if (action.kind === "switchTab") {
      setActiveTab(action.tab);
      navigateRoute({ tab: action.tab });
    } else {
      void performSignOut();
    }
  }

  function discardAndContinue() {
    const action = pendingAction;
    if (!action) return;
    setPendingAction(null);

    if (activeTab === "skills") {
      setSkillDraft(selectedSkillFile?.content ?? "");
    } else {
      setDraft(file?.content ?? "");
    }

    if (action.kind === "open") {
      if (action.path) {
        void openFile(action.path, { keepView: true });
      } else {
        setFile(null);
        setDraft("");
        navigateRoute({ tab: "documents", view }, { replace: true });
      }
    } else if (action.kind === "openSkill") {
      void openSkill(action.id, action.skillFile);
    } else if (action.kind === "switchTab") {
      setActiveTab(action.tab);
      navigateRoute({ tab: action.tab });
    } else {
      void performSignOut();
    }
  }

  function handleFilterChange(nextFilter: string) {
    setFilter(nextFilter);
    const normalizedFilter = nextFilter.trim();
    const currentRoute =
      typeof window !== "undefined" ? parseRoute(window.location) : {};
    navigateRoute(
      {
        filter: normalizedFilter || undefined,
        path: file?.path,
        scenario: currentRoute.scenario,
        tab: "documents",
        view,
      },
      { replace: true },
    );
  }

  function handleViewChange(nextView: WorkspaceView) {
    setView(nextView);
    if (nextView === "info") setDetailsOpen(true);
    const currentRoute =
      typeof window !== "undefined" ? parseRoute(window.location) : {};
    navigateRoute(
      {
        filter: filter || undefined,
        path: file?.path,
        scenario: currentRoute.scenario,
        tab: activeTab,
        view: nextView,
      },
      { replace: true },
    );
  }

  function openHeaderDetails() {
    const nextOpen = !detailsOpen;
    setDetailsOpen(nextOpen);
    let nextView = view;
    if (
      typeof window !== "undefined" &&
      !window.matchMedia("(min-width: 1280px)").matches
    ) {
      nextView = nextOpen ? "info" : defaultView();
      setView(nextView);
    } else if (!nextOpen && view === "info") {
      nextView = defaultView();
      setView(nextView);
    }
    const currentRoute =
      typeof window !== "undefined" ? parseRoute(window.location) : {};
    navigateRoute(
      {
        filter: filter || undefined,
        path: file?.path,
        scenario: currentRoute.scenario,
        tab: activeTab,
        view: nextView,
      },
      { replace: true },
    );
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
        activeTab={activeTab}
        authRequired={authRequired}
        dirty={dirty}
        file={activeTab === "skills" ? null : file}
        filesButtonRef={filesButtonRef}
        git={git}
        onOpenFiles={() => setFilesOpen(true)}
        onSignOut={requestSignOut}
        onTabChange={requestTabChange}
      />

      {workspaceError && (
        <ErrorMessage
          error={workspaceError}
          onRetry={() => void loadWorkspace()}
        />
      )}

      {activeTab === "documents" ? (
        <div
          className={`workspace-layout ${
            detailsOpen
              ? "xl:grid-cols-[clamp(18rem,22vw,22rem)_minmax(0,1fr)_clamp(16rem,20vw,20rem)]"
              : "xl:grid-cols-[clamp(18rem,22vw,22rem)_minmax(0,1fr)]"
          }`}
        >
          <FileNavigator
            entries={tree}
            filter={filter}
            loading={loading}
            mobileOpen={filesOpen}
            onClose={() => {
              setFilesOpen(false);
              filesButtonRef.current?.focus();
            }}
            onFilterChange={handleFilterChange}
            onOpenFile={requestOpenFile}
            onRetry={() => void loadWorkspace()}
            selectedPath={file?.path}
            truncated={treeTruncated}
            workspaceError={workspaceError}
          />
          <DocumentPane
            conflict={conflict}
            detailsOpen={detailsOpen}
            diff={diff}
            documentError={documentError}
            draft={draft}
            file={file}
            fileLoading={fileLoading}
            onCloseFiles={() => setFilesOpen(true)}
            onDownload={() => {
              if (file) window.location.href = api.downloadUrl(file.path);
            }}
            onDraftChange={setDraft}
            onOpenDetails={openHeaderDetails}
            onRetry={() => {
              const pathToRetry =
                file?.path ??
                (typeof window !== "undefined"
                  ? parseRoute(window.location).path
                  : undefined);
              if (pathToRetry) void openFile(pathToRetry, { keepView: true });
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
      ) : (
        <div className="workspace-layout xl:grid-cols-[clamp(18rem,22vw,22rem)_minmax(0,1fr)]">
          <div
            className={`fixed inset-y-0 left-0 z-30 w-72 transform bg-base-100 transition-transform xl:static xl:w-full xl:translate-x-0 ${
              filesOpen ? "translate-x-0" : "-translate-x-full"
            }`}
          >
            <SkillsNavigator
              categories={skillCategories}
              filter={skillsFilter}
              loading={skillsLoading}
              onCreateClick={() => setIsCreateSkillOpen(true)}
              onFilterChange={setSkillsFilter}
              onSelectSkill={requestOpenSkill}
              onTogglePin={(id, pinned) =>
                void handleToggleSkillPin(id, pinned)
              }
              selectedSkillId={selectedSkillId}
              skills={skills}
            />
          </div>

          {filesOpen && (
            <button
              aria-label="Close skills navigator"
              className="fixed inset-0 z-20 bg-black/40 xl:hidden"
              onClick={() => setFilesOpen(false)}
              type="button"
            />
          )}

          {selectedSkillDetail ? (
            <SkillDetailPane
              activeSubTab={activeSkillSubTab}
              draftContent={skillDraft}
              error={skillError}
              isSaving={skillSaving || skillFileLoading}
              onDismissError={() => setSkillError("")}
              onDraftChange={setSkillDraft}
              onResetFile={() => {
                if (selectedSkillFile) setSkillDraft(selectedSkillFile.content);
              }}
              onSaveFile={() => void saveSkillFile()}
              onSelectFile={(f) => void openSkillFile(f)}
              onSubTabChange={setActiveSkillSubTab}
              onToggleEnable={(en) => void handleToggleSkillEnable(en)}
              onTogglePin={(pin) => {
                if (selectedSkillDetail)
                  void handleToggleSkillPin(selectedSkillDetail.id, pin);
              }}
              onViewChange={setView}
              selectedFile={selectedSkillFile}
              skill={selectedSkillDetail}
              view={view}
            />
          ) : (
            <div className="flex h-full flex-col items-center justify-center p-8 text-center bg-base-100">
              <div className="max-w-md space-y-3">
                <div className="mx-auto grid h-12 w-12 place-items-center rounded-full bg-primary/10 text-primary">
                  <svg
                    className="h-6 w-6"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth={1.5}
                    viewBox="0 0 24 24"
                  >
                    <title>Skills Management</title>
                    <path
                      d="M9.813 15.904L9 18.75l-.813-2.846a4.5 4.5 0 00-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 003.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 003.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 00-3.09 3.09z"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                </div>
                <h2 className="text-xl font-bold">Skills Management</h2>
                <p className="text-sm text-base-content/60">
                  Select a skill from the sidebar to inspect its instructions
                  and files, or scaffold a new skill.
                </p>
                <button
                  className="btn btn-primary btn-sm mt-2"
                  onClick={() => setIsCreateSkillOpen(true)}
                  type="button"
                >
                  Create New Skill
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {pendingAction && (
        <DirtyDraftDialog
          actionLabel={
            pendingAction.kind === "open"
              ? "opening another file"
              : pendingAction.kind === "openSkill"
                ? "opening another skill"
                : pendingAction.kind === "switchTab"
                  ? "switching view mode"
                  : "signing out"
          }
          onCancel={() => setPendingAction(null)}
          onDiscard={discardAndContinue}
          onSave={() => void saveAndContinue()}
          saving={saving || skillSaving}
        />
      )}

      <CreateSkillModal
        categories={skillCategories}
        isOpen={isCreateSkillOpen}
        onClose={() => setIsCreateSkillOpen(false)}
        onCreate={handleCreateSkill}
      />
    </main>
  );
}
