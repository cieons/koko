import { createContext, type ReactNode, useContext } from "react";

import type { DeviceSession } from "../api/device";
import type { CurrentSignIn } from "../api/signin";
import type { User, UserCurrentResponse } from "../api/user";

/**
 * Caller is who the web UI acts as, as user.current answers: the User of the
 * Device Session koko web runs as, the User of the Sign-in this browser
 * holds on the web listener, or the Owner, with neither, for the Local
 * caller over the koko server's Unix socket.
 */
export interface Caller {
  user: User;
  deviceSession?: DeviceSession;
  signin?: CurrentSignIn;
  /** owner says the User is the Owner, who sees every page and button. */
  owner: boolean;
}

/** callerOf is the Caller user.current answered. */
export function callerOf({ user, device_session, signin }: UserCurrentResponse): Caller {
  return { user, deviceSession: device_session, signin, owner: user.role === "owner" };
}

export const CallerContext = createContext<Caller | undefined>(undefined);

/** useCaller is who the web UI acts as. Pages render only once it is known. */
export function useCaller(): Caller {
  const caller = useContext(CallerContext);
  if (!caller) {
    throw new Error("a page is outside the shell's CallerContext");
  }
  return caller;
}

/**
 * OwnerOnly shows its children to the Owner alone: a Member never sees what
 * only the Owner may do. The koko server refuses it anyway.
 */
export function OwnerOnly({ children }: { children: ReactNode }) {
  return useCaller().owner ? children : null;
}
