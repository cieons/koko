import { Server } from "lucide-react";

import { lazyPage } from "../../shell/lazyPage";
import type { Route } from "../../shell/router";

/** The host area's pages, each a chunk of its own that loads when first shown. */
export const hostRoutes: Route[] = [
  {
    path: "/hosts",
    page: lazyPage(async () => (await import("./HostsPage")).HostsPage),
    nav: "host.nav",
    icon: Server,
  },
  { path: "/hosts/:host", page: lazyPage(async () => (await import("./HostPage")).HostPage) },
];
