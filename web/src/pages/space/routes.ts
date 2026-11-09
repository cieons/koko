import { Boxes } from "lucide-react";

import type { Route } from "../../shell/router";
import { SpaceDetailPage } from "./SpaceDetailPage";
import { SpaceListPage } from "./SpaceListPage";

/** The space area's pages. */
export const spaceRoutes: Route[] = [
  { path: "/spaces", page: SpaceListPage, nav: "space.nav", icon: Boxes },
  { path: "/spaces/:name", page: SpaceDetailPage },
];
