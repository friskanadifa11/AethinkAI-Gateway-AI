import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The console is served by the gateway itself under /app/, so every asset URL
// must be relative to that prefix. In development Vite serves at the root and
// proxies the API to the locally running gateway, which keeps cookies
// first-party in both modes.
export default defineConfig({
  base: "/app/",
  plugins: [react()],
  build: {
    outDir: "dist",
    sourcemap: false,
    // A single small bundle beats many requests for a console this size.
    chunkSizeWarningLimit: 900,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:8787", changeOrigin: false },
    },
  },
  test: {
    // The console is DOM behaviour: focus handling, dialogs and forms are what
    // break in practice, so the tests run against a real DOM.
    environment: "jsdom",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
  },
});
