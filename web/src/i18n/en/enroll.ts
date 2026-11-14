// The enroll area's messages: its API error codes.
export default {
  "error.unexpected_host_key": "The Host offered a host key other than the one koko expected, so koko did not log in.",
  "error.login_refused": "The Host refused the password or key for its Host Account.",
  "error.host_unreachable": "koko could not connect to the Host.",
  "error.key_not_added": "koko logged in to the Host but could not add the koko key to its authorized_keys.",
  "error.already_managed": "The Host is Managed already; Enroll it again only to pin a new host key.",
  "error.host_changed": "The Host changed while it was being Enrolled, so koko recorded nothing; Enroll it again.",
  "error.passphrase_needed": "The private key is encrypted; give its passphrase.",
  "error.wrong_passphrase": "The passphrase does not open the private key.",
};
