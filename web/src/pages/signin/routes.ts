import { lazyPage } from "../../shell/lazyPage";
import { linkPage, passwordPage, type Route, signInPage } from "../../shell/router";
import { takingLinkSecret } from "./linkSecret";

/** linkPageRoute is a Sign-in Link's page, which takes the link's secret out of the URL. */
const linkPageRoute = takingLinkSecret(lazyPage(async () => (await import("./LinkPage")).LinkPage));

/**
 * signinRoutes are Sign in and a Sign-in Link's page, outside the frame,
 * which a browser opens before it is signed in on the web listener. A link
 * issued before the Sign-in Link opens the same page at /password.
 */
export const signinRoutes: Route[] = [
  { path: signInPage, page: lazyPage(async () => (await import("./SignInPage")).SignInPage), outside: true },
  { path: linkPage, page: linkPageRoute, outside: true },
  { path: passwordPage, page: linkPageRoute, outside: true },
];
