// Code generated from internal/shared/api/space.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

/** Space is a Space as the API returns it. */
export interface Space {
  id: string;
  name: string;
  /**
   * Personal is set for a User's Personal Space, named after them, which
   * cannot be renamed, removed or given Members while its User exists.
   */
  personal: boolean;
}

/** SpaceDetail is a Space with its Members and how many records it holds. */
export interface SpaceDetail {
  id: string;
  name: string;
  personal: boolean;
  /**
   * Members are the Users who are Members of the Space, by name. A
   * Personal Space has none: it belongs to its User, which is not a
   * membership.
   */
  members: SpaceMember[];
  /** Counts are how many records of each kind the Space holds. */
  counts: SpaceCounts;
}

/** SpaceMember is a User who is a Member of a Space. */
export interface SpaceMember {
  /** ID is the User's id. */
  id: string;
  /** Name is the User's name. */
  name: string;
}

/**
 * SpaceCounts are how many records of each kind a Space holds. A Space that
 * holds any cannot be removed.
 */
export interface SpaceCounts {
  hosts: number;
  secrets: number;
}

/** SpaceAddRequest records a shared Space under a new name. */
export interface SpaceAddRequest {
  name: string;
}

/** SpaceAddResponse is the Space space.add recorded. */
export interface SpaceAddResponse {
  space: Space;
}

/** SpaceListRequest asks for every Space the caller can act in. */
export type SpaceListRequest = Record<string, never>;

/** SpaceListResponse holds every Space the caller can act in, by name. */
export interface SpaceListResponse {
  spaces: Space[];
}

/** SpaceShowRequest names a Space to show. */
export interface SpaceShowRequest {
  name: string;
}

/**
 * SpaceShowResponse is the Space space.show names, with its Members and
 * counts.
 */
export interface SpaceShowResponse {
  space: SpaceDetail;
}

/** SpaceRenameRequest gives a shared Space a new name. */
export interface SpaceRenameRequest {
  name: string;
  /** NewName is a name no Space holds. */
  new_name: string;
}

/** SpaceRenameResponse is the Space under its new name. */
export interface SpaceRenameResponse {
  space: Space;
}

/** SpaceRemoveRequest names an empty shared Space to remove. */
export interface SpaceRemoveRequest {
  name: string;
}

/** SpaceRemoveResponse is the Space space.remove removed, as it was. */
export interface SpaceRemoveResponse {
  space: Space;
}

/** SpaceMemberAddRequest makes Users Members of a shared Space. */
export interface SpaceMemberAddRequest {
  /** Space names the Space. */
  space: string;
  /**
   * Users names the Users, at least one. A User who is a Member already
   * stays one.
   */
  users: string[];
}

/** SpaceMemberAddResponse is the Space with its Members after the call. */
export interface SpaceMemberAddResponse {
  space: SpaceDetail;
}

/** SpaceMemberRemoveRequest takes Users out of a Space. */
export interface SpaceMemberRemoveRequest {
  /** Space names the Space. */
  space: string;
  /**
   * Users names the Users, at least one. A User who is not a Member stays
   * out.
   */
  users: string[];
}

/** SpaceMemberRemoveResponse is the Space with its Members after the call. */
export interface SpaceMemberRemoveResponse {
  space: SpaceDetail;
}

/**
 * SpaceAdd records a shared Space. Only the Owner adds one; a name
 * another Space holds is refused with CodeNameTaken.
 */
export const SpaceAdd: Op<SpaceAddRequest, SpaceAddResponse> = {
  name: "space.add",
  path: "/v1/space.add",
  write: true,
};

/** SpaceList lists the Spaces the caller can act in. */
export const SpaceList: Op<SpaceListRequest, SpaceListResponse> = {
  name: "space.list",
  path: "/v1/space.list",
  write: false,
};

/**
 * SpaceShow shows a Space the caller can act in, with its Members and
 * counts. A Space the caller cannot act in is refused exactly as one
 * that does not exist, with CodeUnknownSpace.
 */
export const SpaceShow: Op<SpaceShowRequest, SpaceShowResponse> = {
  name: "space.show",
  path: "/v1/space.show",
  write: false,
};

/**
 * SpaceRename gives a shared Space a new name, which no Space may hold
 * (CodeNameTaken); its id, Members and records stay. Only the Owner
 * renames, and a Personal Space is refused with CodePersonalSpace.
 */
export const SpaceRename: Op<SpaceRenameRequest, SpaceRenameResponse> = {
  name: "space.rename",
  path: "/v1/space.rename",
  write: true,
};

/**
 * SpaceRemove removes a shared Space with its memberships. Only the
 * Owner removes; a Space that holds any record is refused with
 * CodeSpaceNotEmpty, and a Personal Space with CodePersonalSpace.
 */
export const SpaceRemove: Op<SpaceRemoveRequest, SpaceRemoveResponse> = {
  name: "space.remove",
  path: "/v1/space.remove",
  write: true,
};

/**
 * SpaceMemberAdd makes Users Members of a Space, all or none: a name no
 * User holds is refused with CodeUnknownUser and adds nobody. Only the
 * Owner changes Members, and a Personal Space is refused with
 * CodePersonalSpace.
 */
export const SpaceMemberAdd: Op<SpaceMemberAddRequest, SpaceMemberAddResponse> = {
  name: "space.member.add",
  path: "/v1/space.member.add",
  write: true,
};

/**
 * SpaceMemberRemove takes Users out of a Space, all or none, as
 * SpaceMemberAdd adds them. Only the Owner changes Members.
 */
export const SpaceMemberRemove: Op<SpaceMemberRemoveRequest, SpaceMemberRemoveResponse> = {
  name: "space.member.remove",
  path: "/v1/space.member.remove",
  write: true,
};
