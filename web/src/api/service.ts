// Code generated from internal/shared/api/service.go by make generate; DO NOT EDIT.

import type { Op } from "./api";
import type { Filter, SortKey } from "./attr";

/**
 * Service is a Service as the API returns it: what runs on one Host, with
 * the ports it listens on. It lives in its Host's Space and never moves to
 * another Host.
 */
export interface Service {
  id: string;
  /** Name is a lowercase DNS label, unique on its Host. */
  name: string;
  /**
   * Host is the name of the Host the Service runs on, or its id once the
   * Host is Retired and holds its name no more, so that <host>/<service>
   * always names the Service.
   */
  host: string;
  /**
   * Ports are the ports the Service listens on, each written
   * <port>/<protocol>, such as 443/tcp or 51820/udp, by port and then
   * protocol; empty for a Service that listens on none.
   */
  ports: string[];
  /** Attributes are the Service's Attributes, by key, typed as a Host's. */
  attributes: Record<string, string | number>;
  /**
   * Note is the Service's Note, left out when it has none. service.list
   * leaves every Note out unless with_note asks for them.
   */
  note?: string;
  /** CreatedAt and UpdatedAt are RFC 3339 in UTC to the second. */
  created_at: string;
  updated_at: string;
}

/** ServiceAddRequest records a Service on a Host. */
export interface ServiceAddRequest {
  /** Host names the Host, by its name or its id. */
  host: string;
  /** Name is a lowercase DNS label no other Service on the Host holds. */
  name: string;
  /**
   * Ports are the ports it listens on, each <port>/<protocol> or a bare
   * <port> for tcp, none given twice; left out, it listens on none.
   * Other Services' ports are not checked.
   */
  ports?: string[];
  /**
   * Attributes are values by key, each of its key's type (package
   * record): a Preset's, or free text.
   */
  attributes?: Record<string, string | number>;
  /** Note, when given, is the Service's Note: never empty. */
  note?: string;
}

/** ServiceAddResponse is the Service service.add recorded. */
export interface ServiceAddResponse {
  service: Service;
}

/**
 * ServiceListRequest asks for the Services on the Hosts the caller can see
 * that match its filter, in its order.
 */
export interface ServiceListRequest {
  /**
   * Filters are the terms a Service must match, every one. A term takes a
   * field of ServiceListFields: id, name and host, the name of its Host;
   * or an Attribute key of the Service's. A date is absolute.
   */
  filters?: Filter[];
  /**
   * Sort orders the Services by these keys, in turn, from the fields and
   * Attribute keys Filters takes; Services that tie stay by Host, then by
   * name and then id.
   */
  sort?: SortKey[];
  /** WithNote asks for each Service's Note. */
  with_note?: boolean;
}

/**
 * ServiceListResponse holds the Services the caller can see that match the
 * filter, by the sort keys, then by Host name, name and id.
 */
export interface ServiceListResponse {
  services: Service[];
}

/** ServiceShowRequest names a Service to show. */
export interface ServiceShowRequest {
  /** Host names its Host, by its name or its id. */
  host: string;
  /** Name is the Service's name on its Host. */
  name: string;
}

/** ServiceShowResponse is the Service service.show names. */
export interface ServiceShowResponse {
  service: Service;
}

/**
 * ServiceSetRequest changes the ports, Attributes and Note of a Service that
 * it names, and only those.
 */
export interface ServiceSetRequest {
  /** Host names its Host, by its name or its id. */
  host: string;
  /** Name is the Service's name on its Host. */
  name: string;
  /**
   * Ports, when given, replace the Service's ports: at least one, written
   * as on service.add. Unset clears them.
   */
  ports?: string[];
  /** Attributes set these Attributes, by key, leaving the others. */
  attributes?: Record<string, string | number>;
  /** Note, when given, replaces the Service's Note: never empty. */
  note?: string;
  /**
   * Unset clears these Attribute keys, the Note for "note", and every
   * port for "ports". A key the Service lacks is already clear.
   */
  unset?: string[];
}

/** ServiceSetResponse is the Service as service.set left it. */
export interface ServiceSetResponse {
  service: Service;
}

/** ServiceRenameRequest gives a Service a new name on its Host. */
export interface ServiceRenameRequest {
  /** Host names its Host, by its name or its id. */
  host: string;
  /** Name is the Service's name on its Host. */
  name: string;
  /** NewName is a name no other Service on the Host holds. */
  new_name: string;
}

/** ServiceRenameResponse is the Service under its new name. */
export interface ServiceRenameResponse {
  service: Service;
}

/** ServiceRemoveRequest names a Service to delete. */
export interface ServiceRemoveRequest {
  /** Host names its Host, by its name or its id. */
  host: string;
  /** Name is the Service's name on its Host. */
  name: string;
}

/** ServiceRemoveResponse is the Service service.remove deleted, as it was. */
export interface ServiceRemoveResponse {
  service: Service;
}

/**
 * ServiceAdd records a Service on a Host. A name another Service on the
 * Host holds is refused with CodeNameTaken; a value that breaks its
 * rule with CodeInvalidValue.
 */
export const ServiceAdd: Op<ServiceAddRequest, ServiceAddResponse> = {
  name: "service.add",
  path: "/v1/service.add",
  write: true,
};

/** ServiceList lists the Services on every Host the caller can see. */
export const ServiceList: Op<ServiceListRequest, ServiceListResponse> = {
  name: "service.list",
  path: "/v1/service.list",
  write: false,
};

/** ServiceShow shows a Service with its Note. */
export const ServiceShow: Op<ServiceShowRequest, ServiceShowResponse> = {
  name: "service.show",
  path: "/v1/service.show",
  write: false,
};

/**
 * ServiceSet changes the ports, Attributes and Note it names, and only
 * those.
 */
export const ServiceSet: Op<ServiceSetRequest, ServiceSetResponse> = {
  name: "service.set",
  path: "/v1/service.set",
  write: true,
};

/**
 * ServiceRename gives a Service a new name on its Host, refused with
 * CodeNameTaken as service.add refuses one.
 */
export const ServiceRename: Op<ServiceRenameRequest, ServiceRenameResponse> = {
  name: "service.rename",
  path: "/v1/service.rename",
  write: true,
};

/** ServiceRemove deletes a Service, with its Attributes and Note. */
export const ServiceRemove: Op<ServiceRemoveRequest, ServiceRemoveResponse> = {
  name: "service.remove",
  path: "/v1/service.remove",
  write: true,
};

/**
 * CodeUnknownService: the Host, which the caller can see, has no Service of
 * the name given.
 */
export const CodeUnknownService = "unknown_service";
