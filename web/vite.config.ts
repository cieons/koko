import optimizeLocales from "@react-aria/optimize-locales-plugin";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [
    // React Aria ships its own strings in some thirty languages; the web UI
    // speaks two (ADR 0046).
    optimizeLocales.vite({ locales: ["en-US", "zh-CN"] }),
    react(),
    tailwindcss(),
  ],
  build: {
    // The server-only Go package internal/server/webui embeds the build, and
    // git ignores it (ADR 0023). Outside web/, Vite empties it only when told.
    outDir: "../internal/server/webui/site/app",
    emptyOutDir: true,
    // The CSP, default-src 'self', refuses a data: URL, so no asset is
    // inlined as one: each stays a file of its own.
    assetsInlineLimit: 0,
  },
  server: {
    // The static checks read kokod's CSP (static.test.ts), outside web/.
    fs: { allow: [".", decodeURIComponent(new URL("../internal/server/web", import.meta.url).pathname)] },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["src/test/setup.ts"],
    unstubGlobals: true,
    // Vitest leaves a CSS file empty unless it is included here: the static
    // checks read koko's CSS as written (?raw), and the app still imports it
    // empty, since jsdom applies no styles.
    css: { include: [/\.css\?raw$/] },
  },
});
