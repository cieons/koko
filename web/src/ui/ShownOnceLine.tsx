import { Clock } from "lucide-react";
import { useEffect, useState } from "react";

import { formatTimeLeft } from "../shell/format";
import { useLanguage, useT } from "../shell/i18n";
import { CopyButton } from "./copy";

/** ShownOnceLineProps are what a page shows once of a code or link it was just given. */
export interface ShownOnceLineProps {
  /** label names the region, such as "Join Code". */
  label: string;
  /** intro says what the line is for, such as where to run it. */
  intro: string;
  /** line is what is shown once and copied, such as the koko join line. */
  line: string;
  /** validFor is how long it stays valid from when it was issued, in seconds. */
  validFor: number;
  /** expired is what shows in its place once validFor has run out. */
  expired: string;
}

/**
 * ShownOnceLine shows a line a page was just given and shows only once, such
 * as a Join Code's koko join line or a Password Link (docs/web-design.md,
 * "Components"): in an inset block with a dashed accent edge, the line in the
 * code face, which one click selects whole, the time left before it expires,
 * counted down as text, Copy, and the line that it is not shown again. The
 * time left reads in minutes and seconds for its last hour and in days and
 * hours before. Once that time ran out the line goes, and expired says what
 * to do instead. The page keeps what it shows in useShownOnce.
 *
 * The time left counts down from when the line first showed, not to an
 * expiry the koko server stated: the browser's clock may differ from the
 * koko server's, and the line has at most validFor left when it arrives, so
 * it never outlives what it shows.
 */
export function ShownOnceLine({ label, intro, line, validFor, expired }: ShownOnceLineProps) {
  const t = useT();
  const language = useLanguage();
  const [shownAt] = useState(Date.now);
  const now = useNow();
  const left = Math.ceil((shownAt + validFor * 1000 - now) / 1000);
  return (
    <section aria-label={label} className="flex flex-col gap-3">
      {left > 0 ? (
        <>
          <div className="rounded-2xl border border-dashed border-accent bg-accent-soft p-4">
            <div className="mb-2 flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
              <span className="font-semibold">{label}</span>
              <span className="t-num flex items-center gap-1.5 text-sm text-accent-soft-foreground">
                <Clock aria-hidden className="size-4" />
                {t("shell.shownOnce.timeLeft", { time: formatTimeLeft(left, language) })}
              </span>
            </div>
            <code className="t-code block text-[0.9375rem] leading-relaxed whitespace-pre-wrap select-all">{line}</code>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="t-label min-w-0 flex-1 basis-56">
              {intro} {t("shell.shownOnce.notAgain")}
            </p>
            <CopyButton value={line} variant="primary" />
          </div>
        </>
      ) : (
        <p>{expired}</p>
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
