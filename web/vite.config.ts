import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  build: {
    // The server-only Go package internal/server/webui embeds the build, and
    // git ignores it (ADR 0023). Outside web/, Vite empties it only when told.
    outDir: "../internal/server/webui/site/app",
    emptyOutDir: true,
    // The CSP, default-src 'self', refuses a data: URL, so no asset is
    // inlined as one: each stays a file of its own.
    assetsInlineLimit: 0,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["src/test/setup.ts"],
    unstubGlobals: true,
    // Vitest leaves a CSS file empty unless it is included here: the static
    // checks read koko's CSS as written (?raw), and pages still import it
    // empty, since jsdom applies no styles.
    css: { include: [/\.css\?raw$/] },
  },
});
