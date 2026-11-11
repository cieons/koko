/** accessPath is the path of Access, where the panels of a Space and a person open over its lists. */
export const accessPath = "/access";

/** spacePath is the path of a Space's panel, over Access. */
export function spacePath(name: string): string {
  return `${accessPath}/spaces/${encodeURIComponent(name)}`;
}

/** personPath is the path of a person's panel, over Access: the Owner's alone. */
export function personPath(name: string): string {
  return `${accessPath}/people/${encodeURIComponent(name)}`;
}

/**
 * spaceHostsPath is the Hosts list narrowed to a Space, as its Space select
 * writes it; retired narrows it to the Space's Retired Hosts, as its state
 * select does.
 */
export function spaceHostsPath(name: string, retired = false): string {
  const query = new URLSearchParams([["filter", `space=${name}`]]);
  if (retired) {
    query.append("filter", "state=retired");
    query.set("all", "1");
  }
  return `/hosts?${query}`;
}

/** spaceSecretsPath is the Secrets page narrowed to a Space. */
export function spaceSecretsPath(name: string): string {
  return `/secrets?${new URLSearchParams({ space: name })}`;
}
