/**
 * reloadPage loads the browser page again, as after the koko server's
 * upgrade, whose web UI's own files changed with kokod. Tests stand in for
 * it, since jsdom does not navigate.
 */
export function reloadPage(): void {
  window.location.reload();
}
