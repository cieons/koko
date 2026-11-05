import { useEffect, useRef, useState } from "react";

import { CodeRevoked, DeviceRevoke, type DeviceRevokeResponse } from "../api/device";
import { CodeCookieRequired } from "../api/web";
import { ApiError, type Call, DroppedError, observeCalls } from "../client";

/**
 * Notice is what the page says when the web UI cannot act: koko web is not
 * running, the Device Session was Revoked, or the browser's cookie is not one
 * of this run of koko web.
 */
export type Notice = "stopped" | "revoked" | "cookie";

/**
 * nextNotice is the notice after call ended, for a web UI acting through the
 * Device Session ownDeviceSession (none for the Local caller).
 */
function nextNotice(now: Notice | undefined, call: Call, ownDeviceSession: string | undefined): Notice | undefined {
  const { op, response, error } = call;
  if (now === "revoked") {
    return now; // a Revoked Device Session never acts again
  }
  if (error === undefined) {
    // Revoking the Device Session the web UI acts through, from the web UI
    // itself, answers before koko web exits.
    const revoked = op.path === DeviceRevoke.path && (response as DeviceRevokeResponse).device_session.id;
    return ownDeviceSession !== undefined && revoked === ownDeviceSession ? "revoked" : undefined;
  }
  if (error instanceof DroppedError) {
    return "stopped";
  }
  if (error instanceof ApiError) {
    if (error.code === CodeRevoked) {
      return "revoked";
    }
    if (error.code === CodeCookieRequired) {
      return "cookie";
    }
    return undefined; // the koko server answered, so koko web runs
  }
  return now; // an answer that does not read says nothing of koko web
}

/**
 * useNotice is the notice the calls since the shell started call for, for a
 * web UI acting through the Device Session ownDeviceSession. A call that
 * reaches the koko server again clears a stopped koko web's notice, and an
 * ended cookie's once the browser holds a new one; a Revoked Device
 * Session's stays.
 */
export function useNotice(ownDeviceSession: string | undefined): Notice | undefined {
  const [notice, setNotice] = useState<Notice>();
  const own = useRef(ownDeviceSession);
  useEffect(() => {
    own.current = ownDeviceSession;
  }, [ownDeviceSession]);
  useEffect(() => observeCalls((call) => setNotice((now) => nextNotice(now, call, own.current))), []);
  return notice;
}
