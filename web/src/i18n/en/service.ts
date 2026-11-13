// The service area's messages: a Service's panel on its Host's page, and
// the area's API error codes.
export default {
  "service.add": "Add Service",
  "service.on": "Service on {host}",
  "service.portsHint": "Such as 80, 443, 53/udp.",
  "service.portsRule": "Write each port as a number from 1 to 65535, followed by /tcp, /udp or nothing for TCP.",
  "service.readOnly": "Its Host is Retired, so this Service cannot be changed.",
  "service.noAttributes": "No Attribute is recorded.",
  "service.save": "Save",
  "service.added": "{name} is recorded.",
  "service.saved": "{name} is saved.",
  "service.savedNotRenamed": "The changes other than the name are saved; the name is still {name}.",
  "service.remove": "Delete",
  "service.remove.title": "Delete {name}",
  "service.remove.consequence":
    "The record of {name} on {host} is deleted with its Attributes and its Note. Nothing on the Host itself changes.",
  "service.remove.done": "{name} is deleted.",
  "error.unknown_service": "The Host has no Service of that name.",
};
