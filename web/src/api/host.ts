// Code generated from internal/shared/api/host.go by make generate; DO NOT EDIT.

import type { Op } from "./api";
import type { Filter, SortKey } from "./attr";
import type { Service } from "./service";

/** Host is a Host as the API returns it. */
export interface Host {
  id: string;
  /**
   * Name is the Host's SSH Name, a lowercase DNS label unique among the
   * Hosts that are not Retired, whatever Space each is in.
   */
  name: string;
  /** Space is the name of the Space the Host is in. */
  space: string;
  address: string;
  port: number;
  /** Account is the Host Account koko logs in as. */
  account: string;
  /**
   * Relay is the Host the Gateway passes through to reach this one, or
   * null for none. A Relay the caller cannot see is present but unnamed.
   */
  relay: HostRelay | null;
  /** State is HostRegistered, HostManaged or HostRetired. */
  state: string;
  /**
   * HostKey is the pinned host key's SHA-256 fingerprint, as OpenSSH
   * prints it ("SHA256:..."), or null for a Host with no pin.
   */
  host_key: string | null;
  /**
   * Attributes are the Host's Attributes, by key, each value in the JSON
   * type that matches it (package record): a number as a JSON number,
   * written exactly as stored, and every other value as a JSON string.
   */
  attributes: Record<string, string | number>;
  /**
   * Note is the Host's Note, left out when the Host has none. host.list
   * leaves every Note out unless with_note asks for them.
   */
  note?: string;
  /** CreatedAt and UpdatedAt are RFC 3339 in UTC to the second. */
  created_at: string;
  updated_at: string;
}

/**
 * HostRelay is a Host's Relay as the API returns it (ADR 0035): its id and
 * its name, the name it had once it is Retired, as a Host shows its own; both
 * null for a Relay the caller cannot see, which a move may have put in a
 * Space they cannot act in.
 */
export interface HostRelay {
  id: string | null;
  name: string | null;
}

/** NewHost is a Host to record. */
export interface NewHost {
  /** Name is a lowercase DNS label no Host that is not Retired holds. */
  name: string;
  /**
   * Space names the Space to record the Host in; left out, the Host goes
   * to the caller's Personal Space.
   */
  space?: string;
  /** Address is an IP address or a DNS name (package record). */
  address: string;
  /** Port is 1 to 65535; left out, it is 22. */
  port?: number;
  /** Account is the Host Account, which has no default. */
  account: string;
  /**
   * Relay, when given, names the Host's Relay, by its name or its id: a
   * Managed Host the caller can see, with no Relay itself.
   */
  relay?: string;
  /**
   * Attributes are values by key, each of its key's type (package
   * record): a Preset's, or free text.
   */
  attributes?: Record<string, string | number>;
  /** Note, when given, is the Host's Note: never empty. */
  note?: string;
}

/** HostAddRequest records Hosts, all or none. */
export interface HostAddRequest {
  /** Hosts are the Hosts to record, at least one, in one transaction. */
  hosts: NewHost[];
}

/** HostAddResponse is the Hosts host.add recorded, in the order asked. */
export interface HostAddResponse {
  hosts: Host[];
}

/**
 * HostListRequest asks for the Hosts the caller can see that match its
 * filter, in its order.
 */
export interface HostListRequest {
  /**
   * Filters are the terms a Host must match, every one. A term takes a
   * field of HostListFields: id, name, space, address, port (a number),
   * account, relay (the Relay's name), state (an enum: registered,
   * managed, retired) and host_key; or an Attribute key. A date is
   * absolute. A Relay the caller cannot see has an empty name: it
   * matches relay.has and relay.ne, and never relay.eq.
   */
  filters?: Filter[];
  /**
   * Sort orders the Hosts by these keys, in turn, from the fields and
   * Attribute keys Filters takes; Hosts that tie stay by name and then id.
   */
  sort?: SortKey[];
  /** WithNote asks for each Host's Note. */
  with_note?: boolean;
  /**
   * All adds the Retired Hosts, which the filters then take as any other
   * Host: state=retired keeps only them.
   */
  all?: boolean;
}

/**
 * HostListResponse holds the Hosts the caller can see that match the
 * filter, by the sort keys, then by name and then id.
 */
export interface HostListResponse {
  hosts: Host[];
}

/** HostShowRequest names a Host to show, by its name or its id. */
export interface HostShowRequest {
  host: string;
}

/** HostShowResponse is the Host host.show names, with its Services. */
export interface HostShowResponse {
  host: Host;
  /**
   * Services are the Services on the Host, by name in natural order,
   * without their Notes, as service.list answers them.
   */
  services: Service[];
}

/**
 * HostSetRequest changes the fields and Attributes of a Host that it names,
 * or of several, and only those.
 */
export interface HostSetRequest {
  /** Host names the Host, by its name or its id. */
  host?: string;
  /**
   * Hosts, in place of Host, names several Hosts, each by its name or its
   * id, to change alike, all or none (ADR 0050). From revision 2.
   */
  hosts?: string[];
  /** Address, Port and Account, when given, replace the Host's. */
  address?: string;
  port?: number;
  account?: string;
  /** Relay, when given, names the Host's new Relay, as NewHost's does. */
  relay?: string;
  /** Attributes set these Attributes, by key, leaving the others. */
  attributes?: Record<string, string | number>;
  /** Note, when given, replaces the Host's Note: never empty. */
  note?: string;
  /**
   * Unset clears these Attribute keys, the Note for "note", the Relay for
   * "relay", and sets the port back to 22 for "port". A key the Host lacks
   * is already clear.
   */
  unset?: string[];
}

/**
 * HostSetResponse is the Host as host.set left it, or for Hosts each Host
 * as it left them, in the order asked.
 */
export interface HostSetResponse {
  host?: Host;
  hosts?: Host[];
}

/** HostRenameRequest gives a Host a new name, its SSH Name. */
export interface HostRenameRequest {
  /** Host names the Host, by its name or its id. */
  host: string;
  /** NewName is a name no Host that is not Retired holds. */
  new_name: string;
}

/** HostRenameResponse is the Host under its new name. */
export interface HostRenameResponse {
  host: Host;
}

/** HostMoveRequest moves a Host, or several, to another Space. */
export interface HostMoveRequest {
  /** Host names the Host, by its name or its id. */
  host?: string;
  /**
   * Hosts, in place of Host, names several Hosts, each by its name or its
   * id, to move, all or none (ADR 0050). From revision 2.
   */
  hosts?: string[];
  /** Space names the Space to move it to. */
  space: string;
}

/**
 * HostMoveResponse is the Host in its new Space, or for Hosts each Host
 * there, in the order asked.
 */
export interface HostMoveResponse {
  host?: Host;
  hosts?: Host[];
}

/** HostRetireRequest names a Host to retire, or several. */
export interface HostRetireRequest {
  /** Host names the Host, by its name or its id. */
  host?: string;
  /**
   * Hosts, in place of Host, names several Hosts, each by its name or its
   * id, to retire, all or none (ADR 0050). From revision 2.
   */
  hosts?: string[];
}

/**
 * HostRetireResponse is the Host as host.retire left it: Retired, and
 * named by its id alone from then on; or for Hosts each Host so, in the
 * order asked.
 */
export interface HostRetireResponse {
  host?: Host;
  hosts?: Host[];
}

/** HostRemoveRequest names a Host to delete, or several. */
export interface HostRemoveRequest {
  /** Host names the Host, by its name or its id. */
  host?: string;
  /**
   * Hosts, in place of Host, names several Hosts, each by its name or its
   * id, to delete, all or none (ADR 0050). From revision 2.
   */
  hosts?: string[];
}

/**
 * HostRemoveResponse is the Host host.remove deleted, as it was, or for
 * Hosts each Host so, in the order asked.
 */
export interface HostRemoveResponse {
  host?: Host;
  hosts?: Host[];
}

/**
 * HostAdd records Hosts, all or none, each in the Space it names or the
 * caller's Personal Space. A name a Host that is not Retired holds is
 * refused with CodeNameTaken, whatever Space holds it, and with the
 * same message whichever one does; a value that breaks its rule with
 * CodeInvalidValue.
 */
export const HostAdd: Op<HostAddRequest, HostAddResponse> = {
  name: "host.add",
  path: "/v1/host.add",
  write: true,
};

/** HostList lists the Hosts the caller can see. */
export const HostList: Op<HostListRequest, HostListResponse> = {
  name: "host.list",
  path: "/v1/host.list",
  write: false,
};

/** HostShow shows a Host with its Note and its Services. */
export const HostShow: Op<HostShowRequest, HostShowResponse> = {
  name: "host.show",
  path: "/v1/host.show",
  write: false,
};

/**
 * HostSet changes the fields, Attributes and Note it names, and only
 * those, so two callers changing different keys never undo each other.
 * A batch changes each of its Hosts alike; a Host that holds the values
 * already is not at fault, and a request that names nothing to change is
 * refused once, as for one Host.
 */
export const HostSet: Op<HostSetRequest, HostSetResponse> = {
  name: "host.set",
  path: "/v1/host.set",
  write: true,
};

/**
 * HostRename gives a Host a new name, refused with CodeNameTaken as
 * host.add refuses one.
 */
export const HostRename: Op<HostRenameRequest, HostRenameResponse> = {
  name: "host.rename",
  path: "/v1/host.rename",
  write: true,
};

/**
 * HostMove moves a Host, with its Attributes, Note and Services, to
 * another Space; the caller must be able to act in both. Its name stays.
 */
export const HostMove: Op<HostMoveRequest, HostMoveResponse> = {
  name: "host.move",
  path: "/v1/host.move",
  write: true,
};

/**
 * HostRetire makes a Registered or Managed Host Retired, which is
 * terminal. It keeps its record, Attributes, Note, Services, Space and
 * pin, leaves host.list unless all asks for it, and frees its name, so
 * that only its id names it. A Host Retired already is refused with
 * CodeHostRetired, and the Relay of a Host that is not Retired with
 * CodeRelayInUse; in a batch, only Hosts outside it count, so a Relay
 * retires with the Hosts behind it.
 */
export const HostRetire: Op<HostRetireRequest, HostRetireResponse> = {
  name: "host.retire",
  path: "/v1/host.retire",
  write: true,
};

/** HostRemove deletes a Host, with its Attributes, Note and Services. */
export const HostRemove: Op<HostRemoveRequest, HostRemoveResponse> = {
  name: "host.remove",
  path: "/v1/host.remove",
  write: true,
};

/**
 * CodeUnknownHost: no Host the caller can see has the name or id given.
 * A Host in a Space the caller cannot act in answers exactly as one
 * that does not exist.
 */
export const CodeUnknownHost = "unknown_host";

/**
 * CodeInvalidValue: a field, an Attribute or the Note, or a value given
 * for one, breaks its rule; the message names which and why.
 */
export const CodeInvalidValue = "invalid_value";

/**
 * CodeHostWasManaged: host.remove of a Host that has been Managed, as
 * its pinned host key shows: such a Host is kept on record, never
 * deleted.
 */
export const CodeHostWasManaged = "host_was_managed";

/**
 * CodeHostRetired: the call would do what a Retired Host does not take:
 * change its address, port, Host Account or Relay, rename it, Enroll it,
 * retire it again, or change its Services. A Retired Host takes only
 * changes to its Attributes and Note, and moves.
 */
export const CodeHostRetired = "host_retired";

/**
 * CodeInvalidRelay: the Host named as a Relay cannot be one: it is the
 * Host itself, it is not Managed, or it has a Relay itself; or the Host
 * given a Relay is the Relay of a Host that is not Retired. A Relay is
 * exactly one hop. host.enroll refuses with it a Host whose Relay is not
 * Managed, which koko cannot pass through.
 */
export const CodeInvalidRelay = "invalid_relay";

/**
 * CodeRelayInUse: host.retire of a Host that a Host that is not Retired
 * uses as its Relay. The message names neither that Host nor how many.
 */
export const CodeRelayInUse = "relay_in_use";
