import { Button } from "@heroui/react";
import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";

import { useT } from "../shell/i18n";

/** copiedFor is how long Copy reads "Copied" after it copied, in milliseconds. */
export const copiedFor = 1600;

/**
 * writeClipboard puts text on the clipboard. Text still being fetched, as a
 * Secret's value is on Copy, goes in a ClipboardItem where the browser has
 * one, so the click's permission to copy holds while it comes: Safari
 * refuses a writeText made after a wait.
 */
export async function writeClipboard(text: string | Promise<string>): Promise<void> {
  if (typeof text !== "string" && typeof ClipboardItem === "function" && "write" in navigator.clipboard) {
    const blob = text.then((t) => new Blob([t], { type: "text/plain" }));
    await navigator.clipboard.write([new ClipboardItem({ "text/plain": blob })]);
    return;
  }
  await navigator.clipboard.writeText(await text);
}

/** CopyButtonProps are what Copy copies, and how it looks. */
export interface CopyButtonProps {
  /**
   * value is the text to copy, or a function that fetches it on the click,
   * as Copy on a Secret's row calls secret.show; what that function throws
   * goes to onError.
   */
  value: string | (() => Promise<string>);
  /** label names the button where "Copy" alone does not say what it copies, such as "Copy the value of ops/db". */
  label?: string;
  /** failed is what a browser that does not allow copying shows; by default, to select the text and copy it. */
  failed?: string;
  onError?: (error: unknown) => void;
  variant?: "primary" | "secondary" | "tertiary";
  size?: "sm" | "md";
}

/**
 * CopyButton copies value to the clipboard (docs/web-design.md,
 * "Components"). It reads "Copied" for 1.6 s after, announced politely; a
 * browser without the clipboard API, or one that refuses it, says to select
 * the text and copy it by hand instead. A value fetched on the click shows
 * the button pending until it has come.
 */
export function CopyButton({ value, label, failed, onError, variant = "secondary", size = "md" }: CopyButtonProps) {
  const t = useT();
  const [outcome, setOutcome] = useState<"copied" | "failed">();
  const [pending, setPending] = useState(false);
  useEffect(() => {
    if (outcome !== "copied") {
      return;
    }
    const timer = setTimeout(() => setOutcome(undefined), copiedFor);
    return () => clearTimeout(timer);
  }, [outcome]);
  const copy = async () => {
    setOutcome(undefined);
    const text = typeof value === "string" ? value : value();
    const written = writeClipboard(text);
    setPending(typeof text !== "string");
    try {
      await text;
    } catch (error) {
      written.catch(() => undefined); // the value failed, not the clipboard
      onError?.(error);
      return;
    } finally {
      setPending(false);
    }
    try {
      await written;
      setOutcome("copied");
    } catch {
      setOutcome("failed");
    }
  };
  const copied = outcome === "copied";
  return (
    <>
      <Button
        variant={variant}
        size={size}
        aria-label={label}
        isPending={pending}
        onPress={() => void copy()}
        className="shrink-0"
      >
        {copied ? <Check aria-hidden className="size-4" /> : <Copy aria-hidden className="size-4" />}
        <span aria-live="polite">{copied ? t("shell.copied") : t("shell.copy")}</span>
      </Button>
      {outcome === "failed" && (
        <p role="status" className="t-label basis-full">
          {failed ?? t("shell.copyFailed")}
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
