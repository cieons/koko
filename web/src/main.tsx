// Astryx's styles, in the order it requires, then koko's own. The Neutral
// theme's fonts.css is never imported: it loads Figtree from Google Fonts,
// and app.css declares the copy koko bundles (ADR 0045).
import "@astryxdesign/core/reset.css";
import "@astryxdesign/core/astryx.css";
import "@astryxdesign/theme-neutral/theme.css";
import "./shell/app.css";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import { applyStoredMode } from "./shell/mode";

const root = document.getElementById("root");
if (!root) {
  throw new Error("index.html has no #root element");
}
applyStoredMode();
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
