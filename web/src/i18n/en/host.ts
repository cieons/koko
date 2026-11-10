// The host area's messages: a Host's state words, which the shared
// StateMark shows, and its API error codes; its pages add theirs.
export default {
  "host.state.managed": "Managed",
  "host.state.registered": "Registered",
  "host.state.retired": "Retired",
  "error.unknown_host": "No Host you can see has that name.",
  "error.invalid_value": "A field, an Attribute or the Note holds a value it does not take.",
  "error.host_was_managed": "The Host has been Managed, so koko keeps its record and never deletes it.",
  "error.invalid_batch": "Several of the Hosts are at fault, each for its own reason, so none was added.",
  "error.host_retired": "The Host is Retired: it takes only changes to its Attributes and Note, and moves.",
  "error.invalid_relay":
    "That Host cannot be a Relay here: a Relay is a Managed Host other than this one, and is exactly one hop.",
  "error.relay_in_use": "The Host is the Relay of a Host that is not Retired, so it cannot be Retired yet.",
};
