import type { Route } from "./shell/router";

/**
 * routes are the web UI's pages, in the order the top bar links its places;
 * / goes to the first place the caller may see. Each area lists its pages in
 * pages/<area>/routes.ts and adds one line here. The move to HeroUI (ADR
 * 0046) builds the pages again one at a time, so until the first is back
 * every path shows the frame with "Page not found".
 */
export const routes: Route[] = [];
