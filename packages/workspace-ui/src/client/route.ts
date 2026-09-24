import type { WorkspaceView } from "./components";

export interface WorkspaceRoute {
  path?: string | undefined;
  view?: WorkspaceView | undefined;
  filter?: string | undefined;
  scenario?: string | undefined;
}

const validViews: ReadonlySet<string> = new Set([
  "edit",
  "split",
  "preview",
  "info",
]);

function isValidView(value: string | null): value is WorkspaceView {
  return value !== null && validViews.has(value);
}

function safeDecodeURIComponent(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

export function parseRoute(
  locationOrUrl: Location | URL | string,
): WorkspaceRoute {
  let url: URL;
  if (typeof locationOrUrl === "string") {
    url = new URL(locationOrUrl, "http://localhost");
  } else if ("origin" in locationOrUrl && locationOrUrl.origin) {
    url = new URL(locationOrUrl.href);
  } else {
    url = new URL(String(locationOrUrl), "http://localhost");
  }

  const pathname = url.pathname;
  let path: string | undefined;

  if (pathname.startsWith("/files/")) {
    const raw = pathname.slice("/files/".length);
    if (raw) path = safeDecodeURIComponent(raw);
  } else if (pathname === "/files") {
    path = undefined;
  } else if (pathname.startsWith("/file/")) {
    const raw = pathname.slice("/file/".length);
    if (raw) path = safeDecodeURIComponent(raw);
  } else if (pathname === "/file") {
    path = undefined;
  }

  if (!path) {
    const fallbackPath =
      url.searchParams.get("path") ?? url.searchParams.get("file");
    if (fallbackPath) {
      path = fallbackPath.trim();
    }
  }

  if (path) {
    path = path.replace(/^\/+/, "");
  }

  const viewParam = url.searchParams.get("view");
  const view = isValidView(viewParam) ? viewParam : undefined;

  const filterParam =
    url.searchParams.get("filter") ?? url.searchParams.get("q");
  const filter = filterParam ? filterParam : undefined;

  const scenarioParam = url.searchParams.get("scenario");
  const scenario = scenarioParam ? scenarioParam : undefined;

  return {
    filter,
    path: path || undefined,
    scenario,
    view,
  };
}

export function buildRouteUrl(route: WorkspaceRoute): string {
  let pathname = "/";
  if (route.path) {
    const segments = route.path
      .replace(/^\/+/, "")
      .split("/")
      .map(encodeURIComponent);
    pathname = `/files/${segments.join("/")}`;
  }

  const params = new URLSearchParams();
  if (route.scenario) {
    params.set("scenario", route.scenario);
  }
  if (route.view) {
    params.set("view", route.view);
  }
  if (route.filter) {
    params.set("filter", route.filter);
  }

  const query = params.toString();
  return query ? `${pathname}?${query}` : pathname;
}

export function navigateRoute(
  route: WorkspaceRoute,
  options?: { replace?: boolean | undefined },
): void {
  if (typeof window === "undefined" || !window.history) return;
  const targetUrl = buildRouteUrl(route);
  const currentUrl = `${window.location.pathname}${window.location.search}`;
  if (targetUrl === currentUrl) return;

  if (options?.replace) {
    window.history.replaceState(null, "", targetUrl);
  } else {
    window.history.pushState(null, "", targetUrl);
  }
}
