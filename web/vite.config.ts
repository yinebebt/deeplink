import path from "node:path";
import { defineConfig, loadEnv, type ProxyOptions } from "vite";
import react from "@vitejs/plugin-react";

const repoRoot = path.resolve(__dirname, "..");

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, repoRoot, "");
  const apiURL = env.VITE_API_URL || "http://localhost:8090";

  // GET/HEAD stay on Vite (SPA assets); other methods proxy to the Go API
  // so PATCH/DELETE /{shortID} don't collide with the root SPA route.
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
        "/types": apiURL,
        "/stats": apiURL,
        "^/[A-Za-z0-9_-]{6,}$": apiProxy,
      },
    },
  };
});
