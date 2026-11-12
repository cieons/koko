// The files area's messages: the API error codes of the web terminal's
// files endpoints.
export default {
  "error.unknown_tunnel": "This SSH Session is no longer open. Open a new terminal to reach the Host.",
  "error.ssh_session_ended": "This SSH Session has ended, and its file transfers with it.",
  "error.sftp_unavailable": "This Host offers no SFTP, which koko moves files through.",
  "error.host_refused": "The Host refused the request.",
  "error.file_exists": "A file of this name already exists in this directory.",
  "error.bad_offset": "Part of the upload did not arrive. Upload the file again.",
  "error.unknown_upload": "This upload is no longer in progress.",
  "error.too_many_uploads": "This SSH Session is already running the most uploads at once. Wait for one to finish.",
};
