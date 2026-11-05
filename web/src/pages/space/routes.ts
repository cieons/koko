import type { Route } from "../../shell/router";
import { SpaceListPage } from "./SpaceListPage";

/** The space area's pages. */
export const spaceRoutes: Route[] = [{ path: "/spaces", page: SpaceListPage, nav: "space.nav" }];
