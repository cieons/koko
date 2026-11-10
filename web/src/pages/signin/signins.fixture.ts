import { AuthPassword, AuthSignOut, type SignIn, SignInList, SignInRevoke } from "../../api/signin";
import type { User } from "../../api/user";
import { type FakeApi, refusal } from "../../test/fakeApi";

// Sign-ins for the tests of Account and a person's panel: the fake API
// answers signin.list and signin.revoke from them as the koko server would,
// and each write changes them.

/** signIn is a live Sign-in of user from client, with id signin_<user>_<n>. */
export function signIn(user: string, n: number, client: string, more: Partial<SignIn> = {}): SignIn {
  return {
    id: `signin_${user}_${n}`,
    user,
    client,
    address: "203.0.113.7",
    started_at: "2026-11-03T09:00:00Z",
    last_used_at: "2026-11-07T09:00:00Z",
    expires_at: "2026-11-14T09:00:00Z",
    ...more,
  };
}

/** SignIns are what the fake koko server holds of Sign-ins. */
export interface SignIns {
  signins: SignIn[];
}

/**
 * signIns are bob's three: this browser (Firefox on Linux, signin_bob_1),
 * his phone, and one he signed out of; and one of alice's.
 */
export function signIns(): SignIns {
  return {
    signins: [
      signIn("alice", 1, "Safari on macOS"),
      signIn("bob", 1, "Firefox on Linux"),
      signIn("bob", 2, "Safari on iPhone"),
      signIn("bob", 3, "Chrome on Windows", { ended_at: "2026-11-05T09:00:00Z", end_reason: "signed_out" }),
    ],
  };
}

/**
 * serveSignIns makes the fake API answer Sign-ins from s as the koko server
 * answers caller: the Owner sees everyone's, a Member their own. It also
 * answers /auth/password by the current Password, current, ending the
 * caller's other Sign-ins as the koko server does, and /auth/signout.
 */
export function serveSignIns(api: FakeApi, s: SignIns, caller: User, current = "the current password"): void {
  const owner = caller.role === "owner";
  const sees = (si: SignIn) => owner || si.user === caller.name;
  api.on(SignInList, ({ user, all }) => ({
    signins: s.signins.filter(
      (si) => sees(si) && (user === undefined || si.user === user) && (all === true || si.ended_at === undefined),
    ),
  }));
  api.on(SignInRevoke, ({ id }) => {
    const found = s.signins.find((si) => si.id === id && sees(si));
    if (!found) {
      throw refusal("unknown_signin", 404);
    }
    const ended = found.ended_at === undefined ? { ...found, ended_at: "2026-11-08T09:00:00Z", end_reason: "revoked" } : found;
    s.signins = s.signins.map((si) => (si.id === id ? ended : si));
    return { signin: ended };
  });
  api.on(AuthPassword, ({ current: given }) => {
    if (given !== current) {
      throw refusal("wrong_password", 401);
    }
    s.signins = s.signins.map((si) =>
      si.user === caller.name && si.id !== `signin_${caller.name}_1` && si.ended_at === undefined
        ? { ...si, ended_at: "2026-11-08T09:00:00Z", end_reason: "password_changed" }
        : si,
    );
    return { user: caller };
  });
  api.on(AuthSignOut, () => ({}));
}
