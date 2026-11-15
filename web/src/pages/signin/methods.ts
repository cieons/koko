import { useEffect, useState } from "react";

import { AuthMethods, type AuthMethodsResponse } from "../../api/signin";
import { post } from "../../client";
import { passkeysAvailable } from "./webauthn";

/** Methods are the ways to sign in a page on the web listener offers beside a Password. */
export interface Methods {
  /**
   * passkeys says the page offers passkey actions: the browser has WebAuthn
   * (passkeysAvailable) and passkeys work at the Web URL, whose host is no
   * IP address.
   */
  passkeys: boolean;
  /** provider is the Identity Provider's name, as on its button; null while none is configured. */
  provider: string | null;
}

/**
 * useMethods asks /auth/methods which ways to sign in the page offers. It is
 * undefined until the koko server answers, and for a page that asks with
 * enabled false, as one through koko web, which has no /auth/ endpoints, so
 * a page asks it once and hands the answer down. A failed /auth/methods
 * offers passkeys, since the browser's own refusal still shows, and names
 * no provider, whose button would have no name.
 */
export function useMethods(enabled = true): Methods | undefined {
  // null once /auth/methods failed.
  const [answer, setAnswer] = useState<AuthMethodsResponse | null>();
  useEffect(() => {
    if (!enabled) {
      return;
    }
    let latest = true;
    post(AuthMethods, {}).then(
      (methods) => latest && setAnswer(methods),
      () => latest && setAnswer(null),
    );
    return () => {
      latest = false;
    };
  }, [enabled]);
  if (!enabled || answer === undefined) {
    return undefined;
  }
  return {
    passkeys: passkeysAvailable() && (answer?.passkeys ?? true),
    provider: answer?.provider?.name ?? null,
  };
}
