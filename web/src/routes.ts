import { deviceRoutes } from "./pages/device/routes";
import { hostRoutes } from "./pages/host/routes";
import { secretRoutes } from "./pages/secret/routes";
import { spaceRoutes } from "./pages/space/routes";
import { userRoutes } from "./pages/user/routes";
import type { Route } from "./shell/router";

/**
 * routes are the web UI's pages, in the order the header links them; / goes
 * to the first linked page the caller may see. Each area lists its pages in
 * pages/<area>/routes.ts and adds one line here.
 */
export const routes: Route[] = [...hostRoutes, ...secretRoutes, ...spaceRoutes, ...userRoutes, ...deviceRoutes];
