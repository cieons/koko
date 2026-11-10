import { UsersRound } from "lucide-react";

import { lazyPage } from "../../shell/lazyPage";
import { movedTo, type Route } from "../../shell/router";
import { accessPath, spacePath } from "./path";

/** access is Access, one page under each of its paths, so it keeps its state as its panels open and close. */
const access = lazyPage(async () => (await import("./AccessPage")).AccessPage);

/**
 * spaceRoutes are Access, with the panel of a Space and, for the Owner
 * alone, the panel of a person, and the paths of the Spaces and People
 * pages a released koko served, which now go to Access.
 */
export const spaceRoutes: Route[] = [
  { path: accessPath, page: access, nav: "shell.place.access", icon: UsersRound },
  { path: `${accessPath}/spaces/:name`, page: access },
  { path: `${accessPath}/people/:name`, page: access, ownerOnly: true },
  { path: "/spaces", page: movedTo(() => accessPath) },
  { path: "/spaces/:name", page: movedTo(({ name = "" }) => spacePath(name)) },
  { path: "/people", page: movedTo(() => accessPath) },
];
