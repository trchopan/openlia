import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  root: "packages/workspace-ui/src/client",
  build: {
    outDir: "../../dist/public",
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        changeOrigin: true,
        headers: { origin: "http://127.0.0.1:8089" },
        target: "http://127.0.0.1:8089",
      },
      "/health": {
        changeOrigin: true,
        target: "http://127.0.0.1:8089",
      },
    },
  },
});
