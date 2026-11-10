import { useState } from "react";

import { AuthSignOut } from "../api/signin";
import { post } from "../client";
import { navigate, signInPage } from "./router";

/** SignOut is Sign out's call, as the account panel and Account offer it. */
export interface SignOut {
  /** signOut ends this browser's Sign-in and clears its cookie, then goes to Sign in. */
  signOut: () => void;
  pending: boolean;
  /** error is why signing out failed, if it did, to show in place. */
  error?: unknown;
}

/**
 * useSignOut signs this browser out on the web listener: /auth/signout
 * ends its Sign-in and clears the cookie, and the browser goes to Sign in,
 * replacing the history entry, so Back does not return to a page it can no
 * longer read.
 */
export function useSignOut(): SignOut {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();
  const signOut = () => {
    setPending(true);
    setError(undefined);
    post(AuthSignOut, {}).then(
      () => navigate(signInPage, { replace: true }),
      (failure: unknown) => {
        setError(failure);
        setPending(false);
      },
    );
  };
  return { signOut, pending, error };
}
