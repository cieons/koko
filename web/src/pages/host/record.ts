import type { Host } from "../../api/host";
import type { Service } from "../../api/service";
import type { Fields } from "./view";

// What the record's pages share of the record itself: the fields a Host's
// list takes, the paths of a Host's and a Service's pages, and how a form
// reads a port and a Service's ports.

/**
 * hostFields are the fields host.list filters and sorts on, with their
 * types, as api.HostListFields has them; every other key is an Attribute
 * key.
 */
export const hostFields: Fields = {
  id: { type: "text" },
  name: { type: "text" },
  space: { type: "text" },
  address: { type: "text" },
  port: { type: "number" },
  account: { type: "text" },
  relay: { type: "text" },
  state: { type: "enum", values: ["registered", "managed", "retired"] },
  host_key: { type: "text" },
};

/**
 * hostPathOf is the path of the page of the Host that handle names: its
 * name, or its id, which names it wherever a name does.
 */
export function hostPathOf(handle: string): string {
  return `/hosts/${encodeURIComponent(handle)}`;
}

/**
 * hostPath is the path of a Host's page: by its name, or by its id once it
 * is Retired, since a new Host may hold its name.
 */
export function hostPath(host: Host): string {
  return hostPathOf(host.state === "retired" ? host.id : host.name);
}

/**
 * serviceFields are the fields service.list filters and sorts on, with
 * their types, as api.ServiceListFields has them; every other key is an
 * Attribute key.
 */
export const serviceFields: Fields = {
  id: { type: "text" },
  name: { type: "text" },
  host: { type: "text" },
};

/**
 * servicePath is where a Service opens: on its Host's page, the Host by
 * the name or id the Service names it by (docs/web-design.md, "The page
 * map").
 */
export function servicePath(host: string, name: string): string {
  return `${hostPathOf(host)}/services/${encodeURIComponent(name)}`;
}

/** qualified names a Service as every command does: <host>/<service>. */
export function qualified(service: Service): string {
  return `${service.host}/${service.name}`;
}

/**
 * validName tells whether value is a Host's or a Service's name, a
 * lowercase DNS label: 1 to 63 of a-z, 0-9 and '-', not starting or ending
 * with '-' (package name).
 */
export function validName(value: string): boolean {
  return /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(value);
}

/** validPort tells whether value is a port, 1 to 65535. */
export function validPort(value: string): boolean {
  return /^\d{1,5}$/.test(value) && Number(value) >= 1 && Number(value) <= 65535;
}

/** splitPorts reads ports written one after another, separated by commas or spaces. */
export function splitPorts(written: string): string[] {
  return written.split(/[\s,]+/).filter((p) => p !== "");
}
