import type { Route } from "../../shell/router";
import { PeoplePage } from "./PeoplePage";

/** The user area's pages. */
export const userRoutes: Route[] = [{ path: "/people", page: PeoplePage, nav: "user.nav", ownerOnly: true }];
