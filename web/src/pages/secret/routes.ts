import type { Route } from "../../shell/router";
import { SecretsPage } from "./SecretsPage";

/** The secret area's pages. */
export const secretRoutes: Route[] = [{ path: "/secrets", page: SecretsPage, nav: "secret.nav" }];
