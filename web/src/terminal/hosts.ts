import { type Host, HostList } from "../api/host";
import { useRead } from "../shell/data";

/** managedOnly is the filter of host.list for the Managed Hosts, the only ones a terminal reaches. */
const managedOnly = { filters: [{ key: "state", op: "eq", value: "managed" }] };

/**
 * useManagedHosts is the Managed Hosts the caller can see, by name, which
 * the switcher offers and whose Space a tab names, read again as every read
 * is; it reads nothing while not enabled, and is undefined until answered.
 */
export function useManagedHosts(enabled = true): readonly Host[] | undefined {
  return useRead(HostList, managedOnly, { enabled }).data?.hosts;
}
