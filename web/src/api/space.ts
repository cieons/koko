// Code generated from internal/shared/api/space.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

/** Space is a Space as the API returns it. */
export interface Space {
  id: string;
  name: string;
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

/**
 * SpaceAdd records a shared Space. A name another Space holds is refused
 * with CodeNameTaken.
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
