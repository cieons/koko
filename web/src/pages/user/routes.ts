import { lazyPage } from "../../shell/lazyPage";
import { accountPage, movedTo, type Route } from "../../shell/router";

/**
 * userRoutes are Account, which the account panel links, and the path of
 * the Devices page a released koko served, which now goes to it.
 */
export const userRoutes: Route[] = [
  { path: accountPage, page: lazyPage(async () => (await import("./AccountPage")).AccountPage) },
  { path: "/devices", page: movedTo(() => accountPage) },
];
