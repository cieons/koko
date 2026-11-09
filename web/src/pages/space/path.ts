/** spacePath is the path of a Space's page. */
export function spacePath(name: string): string {
  return `/spaces/${encodeURIComponent(name)}`;
}
