import type { Route } from "../../shell/router";
import { HostsPage } from "./HostsPage";

/** The host area's pages. */
export const hostRoutes: Route[] = [{ path: "/hosts", page: HostsPage, nav: "host.nav" }];
