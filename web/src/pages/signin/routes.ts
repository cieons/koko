import { lazyPage } from "../../shell/lazyPage";
import { passwordPage, type Route, signInPage } from "../../shell/router";
import { takingLinkSecret } from "./linkSecret";

/**
 * signinRoutes are Sign in and Set a Password, outside the frame, which a
 * browser opens before it is signed in on the web listener.
 */
export const signinRoutes: Route[] = [
  { path: signInPage, page: lazyPage(async () => (await import("./SignInPage")).SignInPage), outside: true },
  {
    path: passwordPage,
    page: takingLinkSecret(lazyPage(async () => (await import("./PasswordPage")).PasswordPage)),
    outside: true,
  },
];
