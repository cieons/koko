// koko's versions as the koko server names them (spec #136): a release
// such as v0.5.0 or v0.5.0-rc.1, a development build's pseudo-version,
// which sits just before its next release, or "(devel)". The order is
// internal/shared/compat's Compare, which the CLI and the koko server use.

/** devel is the version of a build with no version of its own. */
const devel = "(devel)";

const semver = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/;

/** baseless is a pseudo-version that names no release before it: vX.0.0-<time>-<revision>. */
const baseless = /^v\d+\.0\.0-\d{14}-[0-9a-f]{12}$/;

/**
 * rank places a version as compat's rank does: 2 for "(devel)", 1 for a
 * pseudo-version that names no release before it, 0 for every other.
 */
function rank(version: string): number {
  if (version === devel) {
    return 2;
  }
  return baseless.test(version.replace(/\+.*$/, "")) ? 1 : 0;
}

/** comparePre orders two pre-release parts by semver's rules; an absent one is the release, after every pre-release. */
function comparePre(a: string | undefined, b: string | undefined): number {
  if (a === b) {
    return 0;
  }
  if (a === undefined || b === undefined) {
    return a === undefined ? 1 : -1;
  }
  const as = a.split(".");
  const bs = b.split(".");
  for (let i = 0; i < Math.max(as.length, bs.length); i++) {
    const x = as[i];
    const y = bs[i];
    if (x === undefined || y === undefined) {
      return x === undefined ? -1 : 1;
    }
    const xn = /^\d+$/.test(x);
    const yn = /^\d+$/.test(y);
    if (xn && yn && Number(x) !== Number(y)) {
      return Number(x) < Number(y) ? -1 : 1;
    }
    if (xn !== yn) {
      return xn ? -1 : 1;
    }
    if (x !== y) {
      return x < y ? -1 : 1;
    }
  }
  return 0;
}

/**
 * compareVersions returns -1, 0 or +1 as a is older than, the same as, or
 * newer than b. "(devel)" is the newest, then a pseudo-version that names
 * no release before it; a version that is none of koko's forms is older
 * than every one that is.
 */
export function compareVersions(a: string, b: string): number {
  if (a === b) {
    return 0;
  }
  const ranks = rank(a) - rank(b);
  if (ranks !== 0) {
    return Math.sign(ranks);
  }
  const x = semver.exec(a);
  const y = semver.exec(b);
  if (!x || !y) {
    return x ? 1 : y ? -1 : 0;
  }
  for (const part of [1, 2, 3]) {
    const d = Number(x[part]) - Number(y[part]);
    if (d !== 0) {
      return Math.sign(d);
    }
  }
  return comparePre(x[4], y[4]);
}

/** isNewer tells whether the version a is newer than b. */
export function isNewer(a: string, b: string): boolean {
  return compareVersions(a, b) > 0;
}
