// The server area's messages: the release notice and the koko server's line
// in the account panel, which the shell shows the Owner, and its API error
// codes.
export default {
  "server.release.region": "New koko release",
  "server.release.available": "koko {latest} is available.",
  "server.release.runs": "This koko server runs {version}.",
  "server.release.notes": "Release notes",
  "server.release.later": "Later",
  "server.release.howTo": "How to upgrade",
  "server.release.image.setVersion":
    "Set the release in .env, beside compose.yaml. Skip this step when .env sets KOKO_VERSION=latest.",
  "server.release.image.setVersionLabel": "Line for .env",
  "server.release.image.pull": "Then, in the same folder, pull the new image and start the container again:",
  "server.release.image.pullLabel": "Compose command",
  "server.release.none": "This koko server was started by hand. Replace kokod with the one of {latest}, then start it again.",
  "server.release.upgrade": "Upgrade",
  "server.upgrade.title": "Upgrade the koko server to {version}?",
  "server.upgrade.consequence":
    "It restarts. Every open SSH Session through it ends, and connections through koko web reconnect. Devices keep working; a CLI can be brought to {version} with koko update.",
  "server.upgrade.confirm": "Upgrade and restart",
  "server.upgrade.running": "Upgrading the koko server to {version}…",
  "server.upgrade.succeeded": "The koko server runs {version}. This page reloads.",
  "server.upgrade.failed": "The upgrade to {to} failed. The koko server runs {version}.",
  "server.upgrade.timedOut":
    "The upgrade to {to} has not ended after 15 minutes. On the koko server's machine, this command shows why.",
  "server.upgrade.logLabel": "Command that shows the upgrade's log",
  "server.line.label": "koko server",
  "server.line.checkedAgo": "Checked {ago}",
  "server.line.notChecked": "Not checked yet",
  "server.line.failed": "The last check failed: {error}",
  "server.check.now": "Check now",
  "server.check.checking": "Checking for a new release…",
  "server.check.newest": "{version} is the newest release.",
  "error.already_initialised": "The koko server is initialised already.",
  "error.invalid_address": "That Server Address cannot go into a Join Code.",
  "error.invalid_web_url":
    "The Web URL must be an https:// address with no path, such as https://koko.example.com.",
  "error.update_check_failed": "The koko server could not find the latest koko release.",
  "error.upgrade_unsupported":
    "This koko server cannot upgrade itself in place. Upgrade it the way it was installed.",
  "error.upgrade_running": "An upgrade of the koko server is already in progress.",
};
