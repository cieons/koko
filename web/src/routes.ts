import { hostRoutes } from "./pages/host/routes";
import { secretRoutes } from "./pages/secret/routes";
import type { Route } from "./shell/router";

/**
 * routes are the web UI's pages, in the order the top bar links its places;
 * / goes to the first place the caller may see. Each area lists its pages in
 * pages/<area>/routes.ts and adds one line here. The move to HeroUI (ADR
 * 0046) builds the pages again one at a time, in the places' order: Hosts,
 * Secrets, then Access.
 */
export const routes: Route[] = [...hostRoutes, ...secretRoutes];
