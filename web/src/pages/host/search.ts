import type { Host } from "../../api/host";
import type { Service } from "../../api/service";

// The search of the Hosts page, applied in the browser to what the list
// operation answered: a record matches when any of its values holds the
// text searched for, whatever its case.

/** matches tells whether any of values holds q, already lowercased; "" matches every record. */
export function matches(values: readonly (string | number | undefined)[], q: string): boolean {
  return q === "" || values.some((v) => v !== undefined && String(v).toLocaleLowerCase().includes(q));
}

/**
 * hostValues are what the search matches a Host on (docs/web-design.md,
 * "Hosts"): its name, address, Space, Host Account, Note, every
 * Attribute's value, and its Services with their ports.
 */
export function hostValues(host: Host, services: readonly Service[]): (string | number | undefined)[] {
  return [
    host.name,
    host.address,
    host.space,
    host.account,
    host.note,
    ...Object.values(host.attributes),
    ...services.flatMap((s) => [s.name, ...s.ports]),
  ];
}

/** serviceValues are what the search matches a Service on: its name, its Host and its ports. */
export function serviceValues(service: Service): string[] {
  return [service.name, service.host, ...service.ports];
}

/** byHost are the Services by the Host they run on, as each names it. */
export function byHost(services: readonly Service[]): Map<string, Service[]> {
  const map = new Map<string, Service[]>();
  for (const s of services) {
    map.set(s.host, [...(map.get(s.host) ?? []), s]);
  }
  return map;
}
