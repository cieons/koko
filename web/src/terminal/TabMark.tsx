import { X } from "lucide-react";

import type { MessageKey } from "../i18n/catalogues";
import { useT } from "../shell/i18n";
import type { Status } from "./workspace";

/**
 * marks are each state's mark (docs/web-design.md, "The terminal"): its
 * shape differs with the state, not only its colour.
 */
const marks: Record<Exclude<Status, "refused" | "reconnecting">, string> = {
  connecting: "size-2 rounded-full border-[1.5px] border-muted",
  connected: "size-2 rounded-full bg-ok shadow-[0_0_0_3px_var(--ok-halo)]",
  ended: "h-0.5 w-2 rounded-full bg-muted",
};

/** stateWord is a tab's state in a word. */
export function stateWord(t: (key: MessageKey) => string, status: Status): string {
  return t(`terminal.state.${status}`);
}

/**
 * TabMark is a tab's state as its mark and its word: shown beside it, or,
 * with word false where the tab's name stands beside the mark, for a
 * screen reader alone. Colour never carries the state alone.
 */
export function TabMark({ status, word = true }: { status: Status; word?: boolean }) {
  const t = useT();
  const name = stateWord(t, status);
  return (
    <span className="inline-flex items-center gap-2 text-[0.8125rem] font-medium whitespace-nowrap text-ink-2">
      {status === "refused" ? (
        <X aria-hidden className="size-3 shrink-0 text-danger" strokeWidth={3} />
      ) : status === "reconnecting" ? (
        // A dot half filled: its ring, and its start half.
        <span
          aria-hidden
          className="relative size-2 shrink-0 overflow-hidden rounded-full border-[1.5px] border-[var(--wait)]"
        >
          <span className="absolute inset-y-0 start-0 w-1/2 bg-[var(--wait)]" />
        </span>
      ) : (
        <span aria-hidden className={`shrink-0 ${marks[status]}`} />
      )}
      {word ? name : <span className="sr-only">{name}</span>}
    </span>
  );
}
