import { UsersRound } from "lucide-react";

import { lazyPage } from "../../shell/lazyPage";
import { movedTo, type Route } from "../../shell/router";
import { accessPath, providerPath, spacePath } from "./path";

/** access is Access, one page under each of its paths, so it keeps its state as its panels open and close. */
const access = lazyPage(async () => (await import("./AccessPage")).AccessPage);

/**
 * spaceRoutes are Access, with the panel of a Space and, for the Owner
 * and the Administrators alone, the panels of a person and of the Identity
 * provider, and the paths of the Spaces and People
 * pages a released koko served, which now go to Access.
 */
export const spaceRoutes: Route[] = [
  { path: accessPath, page: access, nav: "shell.place.access", icon: UsersRound, fill: true },
  { path: `${accessPath}/spaces/:name`, page: access, fill: true },
  { path: `${accessPath}/people/:name`, page: access, administers: true, fill: true },
  { path: providerPath, page: access, administers: true, fill: true },
  { path: "/spaces", page: movedTo(() => accessPath, "spaces") },
  { path: "/spaces/:name", page: movedTo(({ name = "" }) => spacePath(name)) },
  { path: "/people", page: movedTo(() => accessPath) },
];
