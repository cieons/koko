// The ids of the SSH Sessions a browser tab's terminals hold, kept in
// sessionStorage so the document after a reload, or after the browser
// discarded the tab and restored it, can end them before it opens its own
// (P1 spec: a terminal in the web UI, "Resuming from the page"). It is a
// module of its own so the shell can tell whether there are any without
// loading the terminal.

/** keptKey is where sessionStorage keeps the ids, as a JSON array. */
const keptKey = "koko.terminal.sessions";

/** keptSessions is the ids sessionStorage keeps: none when it cannot be read. */
export function keptSessions(): string[] {
  try {
    const kept: unknown = JSON.parse(window.sessionStorage.getItem(keptKey) ?? "[]");
    return Array.isArray(kept) ? kept.filter((id) => typeof id === "string") : [];
  } catch {
    return [];
  }
}

/** keepSessions writes the kept ids as change makes them. */
export function keepSessions(change: (ids: string[]) => string[]): void {
  try {
    const ids = change(keptSessions());
    if (ids.length > 0) {
      window.sessionStorage.setItem(keptKey, JSON.stringify(ids));
    } else {
      window.sessionStorage.removeItem(keptKey);
    }
  } catch {
    // A browser that refuses storage keeps no ids, and a reload ends none.
  }
}

/** forgetSession takes one id out of the kept ones. */
export function forgetSession(id: string): void {
  keepSessions((ids) => ids.filter((kept) => kept !== id));
}
