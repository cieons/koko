import type { Route } from "../../shell/router";
import { DevicesPage } from "./DevicesPage";

/** The device area's pages. */
export const deviceRoutes: Route[] = [{ path: "/devices", page: DevicesPage, nav: "device.nav" }];
