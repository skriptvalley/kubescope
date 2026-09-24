import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Dev-mode proxy target: the Go backend (make dev runs both).
const backend = "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      // ws:true upgrades the exec WebSocket (Sprint 6) through the dev proxy;
      // the backend authorizes localhost origins so make dev works end-to-end.
      "/api": { target: backend, ws: true },
      "/healthz": backend,
    },
  },
  // Relative asset URLs: the server's injected <base href> (KUBESCOPE_BASE_PATH,
  // ADR-0012) anchors them, so one build serves at the root or under a sub-path.
  base: "./",
  build: {
    outDir: "dist",
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
});
