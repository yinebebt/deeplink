import path from "node:path";
import { defineConfig, loadEnv, type ProxyOptions } from "vite";
import react from "@vitejs/plugin-react";

// The repo's root .env is the single source of truth — Vite reads VITE_*
// vars from there so the frontend shares one config file with the Go
// service.
const repoRoot = path.resolve(__dirname, "..");

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, repoRoot, "");
  const apiURL = env.VITE_API_URL || "http://localhost:8091";

  // The Go API uses /{shortID} for PATCH and DELETE, which clashes with
  // Vite's root-path SPA serving. The bypass callback lets GETs (HTML,
  // JS, CSS, assets) stay with Vite and forwards every other method
  // straight to the Go server.
  const apiProxy: ProxyOptions = {
    target: apiURL,
    changeOrigin: true,
    bypass: (req) => {
      if (req.method === "GET" || req.method === "HEAD") return req.url;
      return null;
    },
  };

  return {
    base: "/",
    envDir: repoRoot,
    plugins: [react()],
    build: { outDir: "dist", emptyOutDir: true },
    server: {
      port: 5173,
      proxy: {
        "/shorten": apiURL,
        "/links": apiURL,
        // PATCH/DELETE /{shortID} go through the API; GETs stay with Vite.
        "^/[A-Za-z0-9_-]{6,}$": apiProxy,
      },
    },
  };
});
