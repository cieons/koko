import { KeyRound } from "lucide-react";

import { lazyPage } from "../../shell/lazyPage";
import type { Route } from "../../shell/router";

/** The secret area's pages, each a chunk of its own that loads when first shown. */
export const secretRoutes: Route[] = [
  {
    path: "/secrets",
    page: lazyPage(async () => (await import("./SecretsPage")).SecretsPage),
    nav: "shell.place.secrets",
    icon: KeyRound,
    fill: true,
  },
];
