import { Button } from "@heroui/react";
import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";

import { useT } from "../shell/i18n";

/** copiedFor is how long Copy reads "Copied" after it copied, in milliseconds. */
export const copiedFor = 1600;

/**
 * CopyButton copies value to the clipboard (docs/web-design.md,
 * "Components"). It reads "Copied" for 1.6 s after, announced politely; a
 * browser without the clipboard API, or one that refuses it, says to select
 * the text and copy it by hand instead.
 */
export function CopyButton({ value, variant = "secondary" }: { value: string; variant?: "primary" | "secondary" }) {
  const t = useT();
  const [outcome, setOutcome] = useState<"copied" | "failed">();
  useEffect(() => {
    if (outcome !== "copied") {
      return;
    }
    const timer = setTimeout(() => setOutcome(undefined), copiedFor);
    return () => clearTimeout(timer);
  }, [outcome]);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setOutcome("copied");
    } catch {
      setOutcome("failed");
    }
  };
  const copied = outcome === "copied";
  return (
    <>
      <Button variant={variant} onPress={() => void copy()} className="shrink-0">
        {copied ? <Check aria-hidden className="size-4" /> : <Copy aria-hidden className="size-4" />}
        <span aria-live="polite">{copied ? t("shell.copied") : t("shell.copy")}</span>
      </Button>
      {outcome === "failed" && (
        <p role="status" className="t-label basis-full">
          {t("shell.copyFailed")}
        </p>
      )}
    </>
  );
}

/**
 * CommandLine is a command to copy and run elsewhere, such as ssh web1 or
 * koko host enroll web1 (docs/web-design.md, "Components"): a $, the command
 * in the code face, which one click selects whole, and Copy, in an inset
 * block. label names the command, such as "SSH command".
 */
export function CommandLine({ command, label }: { command: string; label: string }) {
  return (
    <div
      role="group"
      aria-label={label}
      className="flex flex-wrap items-center gap-3 rounded-xl border border-border bg-background py-1.5 pr-1.5 pl-4"
    >
      <code className="t-code min-w-0 flex-1 py-1.5 text-sm select-all">
        <span aria-hidden className="mr-2 text-muted select-none">
          $
        </span>
        {command}
      </code>
      <CopyButton value={command} />
    </div>
  );
}
