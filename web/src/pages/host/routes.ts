import { Server } from "lucide-react";
import { useEffect } from "react";

import { lazyPage } from "../../shell/lazyPage";
import { navigate, type PageProps, type Route, useLocation } from "../../shell/router";
import { servicePath } from "./record";

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

/**
 * ServiceMoved sends /services/<host>/<service>, a Service's page a
 * released koko served, to the Service's panel on its Host's page, keeping
 * its query and replacing the history entry.
 */
function ServiceMoved({ params }: PageProps) {
  const { search } = useLocation();
  const to = servicePath(params["host"] ?? "", params["service"] ?? "") + search;
  useEffect(() => navigate(to, { replace: true }), [to]);
  return null;
}

/**
 * hostPage is a Host's page, at /hosts/<host>, with a Service's panel open
 * over it at /hosts/<host>/services/<service>: one component for both, so
 * opening and closing the panel keeps the page as it is.
 */
const hostPage = lazyPage(() => import("./HostPage").then((m) => m.HostPage));

/**
 * hostRoutes are the record's pages: Hosts, with its Services view, a
 * Host's page with a Service's panel over it, and the redirects from the
 * paths a released koko served.
 */
export const hostRoutes: Route[] = [
  {
    path: "/hosts",
    page: lazyPage(() => import("./HostsPage").then((m) => m.HostsPage)),
    nav: "shell.place.hosts",
    icon: Server,
  },
  { path: "/hosts/:host", page: hostPage },
  { path: "/hosts/:host/services/:service", page: hostPage },
  { path: "/services", page: ServicesMoved },
  { path: "/services/:host/:service", page: ServiceMoved },
];
