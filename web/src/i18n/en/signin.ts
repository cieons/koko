// The signin area's messages: its API error codes.
export default {
  "error.web_url_unset":
    "A Password Link requires the Web URL, which is not set. The Owner sets it with koko server set web_url= on the koko server's machine.",
  "error.invalid_password":
    "The Password must be 15 to 128 characters long and must not be the User name. Any characters are allowed.",
  "error.wrong_password": "The User name or the Password is incorrect.",
  "error.too_many_attempts": "Too many failed attempts. Try again later; the wait is at most 15 minutes.",
  "error.link_used": "This Password Link has already been used. Ask the Owner for a new one.",
  "error.link_invalid": "This Password Link has expired or is no longer valid. Ask the Owner for a new one.",
  "error.unknown_signin": "No Sign-in has that ID.",
  "error.no_password": "That User has no Password.",
};
