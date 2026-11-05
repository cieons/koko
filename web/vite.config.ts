import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  build: {
    // The server-only Go package internal/server/webui embeds the build, and
    // git ignores it (ADR 0023). Outside web/, Vite empties it only when told.
    outDir: "../internal/server/webui/site/app",
    emptyOutDir: true,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["src/test/setup.ts"],
    unstubGlobals: true,
  },
});
