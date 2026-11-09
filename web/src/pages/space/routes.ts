import { Boxes } from "lucide-react";

import { lazyPage } from "../../shell/lazyPage";
import type { Route } from "../../shell/router";

/** The space area's pages, each a chunk of its own that loads when first shown. */
export const spaceRoutes: Route[] = [
  {
    path: "/spaces",
    page: lazyPage(async () => (await import("./SpaceListPage")).SpaceListPage),
    nav: "space.nav",
    icon: Boxes,
  },
  { path: "/spaces/:name", page: lazyPage(async () => (await import("./SpaceDetailPage")).SpaceDetailPage) },
];
