import { Card } from "@astryxdesign/core/Card";
import { VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { useEffect, useState } from "react";

import { CopyLine } from "./CopyLine";
import { formatTimeLeft } from "./format";
import { useLanguage, useT } from "./i18n";

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
 * as a Join Code's koko join line: the line to copy (CopyLine), and the
 * time left before it expires, which the page counts down as text, in
 * minutes and seconds for the last hour and in days and hours before. Once that time ran out it shows the line no more. The page keeps
 * what it shows in useShownOnce.
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
    <Card role="region" aria-label={label} padding={4}>
      {left > 0 ? (
        <VStack gap={3}>
          <Text type="body">{intro}</Text>
          <CopyLine line={line} />
          <Text type="body" color="secondary">
            {t("shell.shownOnce.expiresIn", { time: formatTimeLeft(left, language) })}
          </Text>
        </VStack>
      ) : (
        <Text type="body">{expired}</Text>
      )}
    </Card>
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
