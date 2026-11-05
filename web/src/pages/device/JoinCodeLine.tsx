import "./device.css";

import { useEffect, useState } from "react";
import { flushSync } from "react-dom";

import type { JoinCode } from "../../api/device";
import { useT } from "../../shell/i18n";

/**
 * joinCodeLifetime is how long a Join Code works from its issue: 15 minutes,
 * a constant of the koko server (P0 spec: people and Spaces).
 */
const joinCodeLifetime = 15 * 60_000;

/**
 * useShownOnce holds what a page shows once, such as a Join Code just
 * issued. It lives in the page's state alone, never in the URL or the
 * browser's storage, so leaving the page within the web UI loses it; and it
 * is forgotten when the browser leaves the page (pagehide), so a page the
 * browser keeps to come back to holds it no more.
 */
export function useShownOnce<T>(): [T | undefined, (value: T) => void] {
  const [value, setValue] = useState<T>();
  useEffect(() => {
    const forget = () => flushSync(() => setValue(undefined));
    window.addEventListener("pagehide", forget);
    return () => window.removeEventListener("pagehide", forget);
  }, []);
  return [value, setValue];
}

/**
 * JoinCodeLine shows a Join Code just issued, by an Invite or an added
 * device: the koko join line to paste on the new device, a button that
 * copies it, and the time left before it expires. Once that time ran out it
 * shows the code no more; another means issuing a new one.
 *
 * The time left counts down from when the line first showed, not to the
 * code's expires_at: the browser's clock may differ from the koko server's,
 * and a code has at most its 15 minutes left when it arrives, so the line
 * never goes before the code does.
 */
export function JoinCodeLine({ joinCode }: { joinCode: JoinCode }) {
  const t = useT();
  const [shownAt] = useState(Date.now);
  const now = useNow();
  const [copied, setCopied] = useState<boolean>();
  const left = Math.ceil((shownAt + joinCodeLifetime - now) / 1000);
  const line = `koko join ${joinCode.code}`;
  // A browser without the clipboard API, or one that refuses it, still
  // shows the line, which a click selects whole.
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(line);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <section className="join-code" aria-label={t("device.joinCode.title")}>
      {left > 0 ? (
        <>
          <p>{t("device.joinCode.intro", { user: joinCode.user, device: joinCode.device })}</p>
          <p>
            <code className="join-code-line">{line}</code>{" "}
            <button type="button" onClick={() => void copy()}>
              {t("device.joinCode.copy")}
            </button>{" "}
            {copied !== undefined && (
              <span role="status">{copied ? t("device.joinCode.copied") : t("device.joinCode.copyFailed")}</span>
            )}
          </p>
          <p>{t("device.joinCode.expiresIn", { time: minutesAndSeconds(left) })}</p>
        </>
      ) : (
        <p>{t("device.joinCode.expired")}</p>
      )}
    </section>
  );
}

/** useNow is the browser's clock, read again every second. */
function useNow(): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(tick);
  }, []);
  return now;
}

/** minutesAndSeconds shows a number of seconds as m:ss, such as 14:59. */
function minutesAndSeconds(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}
