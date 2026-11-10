import { Server } from "lucide-react";
import { useEffect } from "react";

import { lazyPage } from "../../shell/lazyPage";
import { navigate, type Route, useLocation } from "../../shell/router";

/**
 * ServicesMoved sends /services, the Services list a released koko served,
 * to the Services view of the Hosts page, keeping its query and replacing
 * the history entry (docs/web-design.md, "The page map").
 */
function ServicesMoved() {
  const { search } = useLocation();
  useEffect(() => {
    const query = new URLSearchParams(search);
    query.set("view", "services");
    navigate(`/hosts?${query}`, { replace: true });
  }, [search]);
  return null;
}

/** hostRoutes are the record's list pages: Hosts, with its Services view, and the redirect from /services. */
export const hostRoutes: Route[] = [
  {
    path: "/hosts",
    page: lazyPage(() => import("./HostsPage").then((m) => m.HostsPage)),
    nav: "shell.place.hosts",
    icon: Server,
  },
  { path: "/services", page: ServicesMoved },
];
