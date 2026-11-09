import { Server } from "lucide-react";

import type { Route } from "../../shell/router";
import { HostPage } from "./HostPage";
import { HostsPage } from "./HostsPage";

/** The host area's pages. */
export const hostRoutes: Route[] = [
  { path: "/hosts", page: HostsPage, nav: "host.nav", icon: Server },
  { path: "/hosts/:host", page: HostPage },
];
