import { MonitorSmartphone } from "lucide-react";

import { lazyPage } from "../../shell/lazyPage";
import type { Route } from "../../shell/router";

/** The device area's pages, each a chunk of its own that loads when first shown. */
export const deviceRoutes: Route[] = [
  {
    path: "/devices",
    page: lazyPage(async () => (await import("./DevicesPage")).DevicesPage),
    nav: "device.nav",
    icon: MonitorSmartphone,
  },
];
