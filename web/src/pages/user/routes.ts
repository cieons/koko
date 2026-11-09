import { Users } from "lucide-react";

import { lazyPage } from "../../shell/lazyPage";
import type { Route } from "../../shell/router";

/** The user area's pages, each a chunk of its own that loads when first shown. */
export const userRoutes: Route[] = [
  {
    path: "/people",
    page: lazyPage(async () => (await import("./PeoplePage")).PeoplePage),
    nav: "user.nav",
    icon: Users,
    ownerOnly: true,
  },
];
