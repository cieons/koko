// The theme: HeroUI's styles for the components in use, then Obsidian's
// tokens, type and panel (ADR 0046).
import "./theme/theme.css";

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
