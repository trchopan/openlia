import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";

const clientRoot = fileURLToPath(new URL("./src/client", import.meta.url));
const publicOutput = fileURLToPath(new URL("./dist/public", import.meta.url));
const backendUrl =
  process.env.OPENLIA_WORKSPACE_UI_BACKEND_URL ?? "http://127.0.0.1:8089";
const backendOrigin = new URL(backendUrl).origin;

export default defineConfig({
  plugins: [react(), tailwindcss()],
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
      "/api": {
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
});
