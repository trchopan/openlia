import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";
import { fileURLToPath } from "node:url";

const clientRoot = fileURLToPath(new URL("./src/client", import.meta.url));
const publicOutput = fileURLToPath(new URL("./dist/public", import.meta.url));
const backendUrl =
  process.env.OPENLIA_WORKSPACE_UI_BACKEND_URL ?? "http://127.0.0.1:8089";
const backendOrigin = new URL(backendUrl).origin;

function developmentCspPlugin(): Plugin {
  return {
    name: "workspace-ui-development-csp",
    transformIndexHtml(html) {
      return html.replace(
        "style-src 'self'",
        "style-src 'self' 'unsafe-inline'",
      );
    },
  };
}

export default defineConfig(({ command }) => ({
  plugins: [
    react(),
    tailwindcss(),
    ...(command === "serve" ? [developmentCspPlugin()] : []),
  ],
  root: clientRoot,
  build: {
    outDir: publicOutput,
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    host: process.env.OPENLIA_WORKSPACE_UI_DEV_HOST ?? "127.0.0.1",
    port: Number(process.env.OPENLIA_WORKSPACE_UI_DEV_PORT ?? "5173"),
    proxy: {
      "/api/": {
        changeOrigin: true,
        headers: { origin: backendOrigin },
        target: backendUrl,
      },
      "/health": {
        changeOrigin: true,
        target: backendUrl,
      },
    },
  },
}));
