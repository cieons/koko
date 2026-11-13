import { createContext, type ReactNode, useContext } from "react";

import type { DeviceSession } from "../api/device";
import type { CurrentSignIn } from "../api/signin";
import type { User, UserCurrentResponse } from "../api/user";
import type { MessageKey } from "../i18n/catalogues";

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
  /** owner says the User is the Owner, the one who acts on Administrators. */
  owner: boolean;
  /**
   * administers says the User is the Owner or an Administrator, who see
   * every page and every Space and act on every Member.
   */
  administers: boolean;
}

/** callerOf is the Caller user.current answered. */
export function callerOf({ user, device_session, signin }: UserCurrentResponse): Caller {
  return {
    user,
    deviceSession: device_session,
    signin,
    owner: user.role === "owner",
    administers: user.role === "owner" || user.role === "administrator",
  };
}

/** roleName is the message that names a User's role: Owner, Administrator or Member. */
export function roleName(user: User): MessageKey {
  switch (user.role) {
    case "owner":
      return "shell.role.owner";
    case "administrator":
      return "shell.role.administrator";
    default:
      return "shell.role.member";
  }
}

/**
 * mayActOn tells whether caller may act on user's credentials and role, as
 * the koko server's access check answers it (ADR 0056): anyone on
 * themselves, the Owner on anyone, and an Administrator on a Member.
 */
export function mayActOn(caller: Caller, user: User): boolean {
  return caller.user.name === user.name || caller.owner || (caller.administers && user.role === "member");
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
 * Administers shows its children to the Owner and the Administrators alone:
 * a Member never sees what only they may do. The koko server refuses it
 * anyway.
 */
export function Administers({ children }: { children: ReactNode }) {
  return useCaller().administers ? children : null;
}
