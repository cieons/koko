import { expect, test } from "vitest";

import { isNewer } from "./version";

// The order of koko's versions, as internal/shared/compat's Compare gives it.

test("a release is newer than an older release, by number, not by text", () => {
  expect(isNewer("v0.5.0", "v0.4.2")).toBe(true);
  expect(isNewer("v0.10.0", "v0.9.0")).toBe(true);
  expect(isNewer("v1.0.0", "v0.99.99")).toBe(true);
  expect(isNewer("v0.4.2", "v0.5.0")).toBe(false);
  expect(isNewer("v0.5.0", "v0.5.0")).toBe(false);
});

test("a release is newer than its pre-releases and its development builds", () => {
  expect(isNewer("v0.5.0", "v0.5.0-rc.1")).toBe(true);
  expect(isNewer("v0.5.0-rc.2", "v0.5.0-rc.1")).toBe(true);
  expect(isNewer("v0.5.0-rc.10", "v0.5.0-rc.9")).toBe(true);
  expect(isNewer("v0.5.0", "v0.5.0-0.20261111093000-abcdefabcdef")).toBe(true);
  expect(isNewer("v0.5.0", "v0.4.3-0.20261111093000-abcdefabcdef+dirty")).toBe(true);
  expect(isNewer("v0.4.2", "v0.4.3-0.20261111093000-abcdefabcdef")).toBe(false);
});

test("no release is newer than (devel) or a build that names no release before it", () => {
  expect(isNewer("v9.0.0", "(devel)")).toBe(false);
  expect(isNewer("v9.0.0", "v0.0.0-20261111093000-abcdefabcdef")).toBe(false);
});

test("a version that is none of koko's forms is older than every release", () => {
  expect(isNewer("v0.5.0", "unknown")).toBe(true);
  expect(isNewer("unknown", "v0.5.0")).toBe(false);
});
