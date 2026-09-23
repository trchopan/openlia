import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { createMockWorkspaceApi } from "./mockApi";
import { httpWorkspaceApi } from "./api";

const mockScenario = import.meta.env.VITE_WORKSPACE_UI_SCENARIO;
const requestedScenario = new URLSearchParams(window.location.search).get(
  "scenario",
);
const api =
  import.meta.env.DEV && import.meta.env.VITE_WORKSPACE_UI_MODE !== "real"
    ? createMockWorkspaceApi({
        scenario:
          requestedScenario === "auth" ||
          requestedScenario === "conflict" ||
          requestedScenario === "empty" ||
          requestedScenario === "error" ||
          requestedScenario === "loading"
            ? requestedScenario
            : mockScenario === "auth" ||
                mockScenario === "conflict" ||
                mockScenario === "empty" ||
                mockScenario === "error" ||
                mockScenario === "loading"
              ? mockScenario
              : "default",
      })
    : httpWorkspaceApi;

const root = document.querySelector("#app");
if (!root) throw new Error("workspace UI root element is missing");

createRoot(root).render(
  <StrictMode>
    <App api={api} />
  </StrictMode>,
);
