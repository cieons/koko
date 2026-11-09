import { Plug } from "lucide-react";

import { lazyPage } from "../../shell/lazyPage";
import type { Route } from "../../shell/router";

/** The service area's pages, each a chunk of its own that loads when first shown. */
export const serviceRoutes: Route[] = [
  {
    path: "/services",
    page: lazyPage(async () => (await import("./ServicesPage")).ServicesPage),
    nav: "service.nav",
    icon: Plug,
  },
  { path: "/services/:host/:name", page: lazyPage(async () => (await import("./ServicePage")).ServicePage) },
];
