import type { Route } from "../../shell/router";
import { ServicePage } from "./ServicePage";
import { ServicesPage } from "./ServicesPage";

/** The service area's pages. */
export const serviceRoutes: Route[] = [
  { path: "/services", page: ServicesPage, nav: "service.nav" },
  { path: "/services/:host/:name", page: ServicePage },
];
