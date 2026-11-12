import { useEffect, useRef } from "react";

import { useT } from "../shell/i18n";
import { lazyPart } from "../shell/lazyPage";
import { navigate, type PageProps, type Route, useLocation } from "../shell/router";
import { notify } from "../ui/notify";
import type { WorkspaceProps } from "./TerminalPage";
import { addTab, loadWorkspace, maxTabs, terminalPath, workspace, workspaceQuery } from "./workspace";

// The terminal's pages (docs/web-design.md, "The page map"). The workspace
// is no page the router swaps out: the frame keeps it mounted from the first
// terminal on, so these pages only have it loaded, and add a tab.

/** WorkspaceLayer is the terminal workspace, a chunk of its own, which the frame mounts once it is wanted. */
export const WorkspaceLayer = lazyPart<WorkspaceProps>(() => import("./TerminalPage").then((module) => module.Workspace));

/** TerminalRoute is /terminal: the frame shows the workspace on it, which it has loaded. */
function TerminalRoute() {
  useEffect(loadWorkspace, []);
  return null;
}

/**
 * HostTerminal is /hosts/<name>/terminal, which a Managed Host's Open
 * terminal links to: it adds a tab for that Host to the workspace, once
 * per visit, and replaces itself with the workspace's URL. At 16 tabs it
 * adds none, and says why.
 */
function HostTerminal({ params }: PageProps) {
  const t = useT();
  const { visit } = useLocation();
  const added = useRef<number>(undefined);
  const name = params.name ?? "";
  useEffect(() => {
    if (added.current !== visit) {
      added.current = visit;
      if (addTab(name) === undefined) {
        notify(t("terminal.tab.limit", { max: maxTabs }));
      }
      loadWorkspace();
      navigate(terminalPath + workspaceQuery(workspace()), { replace: true });
    }
  }, [visit, name, t]);
  return null;
}

export const terminalRoutes: Route[] = [
  { path: terminalPath, page: TerminalRoute },
  { path: "/hosts/:name/terminal", page: HostTerminal },
];
