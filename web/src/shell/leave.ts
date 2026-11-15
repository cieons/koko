/**
 * leaveFor sends the browser to location, a page of another site, as to the
 * Identity Provider to sign in there. Tests stand in for it, since jsdom
 * does not navigate.
 */
export function leaveFor(location: string): void {
  window.location.assign(location);
}
