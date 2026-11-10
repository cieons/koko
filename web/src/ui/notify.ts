import { toast } from "@heroui/react";

/**
 * notify says what a call that changed something did, such as "web1 is
 * Retired.", in a toast at the bottom end (docs/web-design.md,
 * "Components"). A refusal never shows as a toast: it shows in place
 * (Refusal), and the shell's notices across the top.
 */
export function notify(message: string): void {
  toast.success(message);
}
