import { useT } from "../shell/i18n";

/** marks are each state's mark: its shape differs with the state, not only its colour. */
const marks = {
  managed: "size-2 rounded-full bg-ok shadow-[0_0_0_3px_var(--ok-halo)]",
  registered: "size-2 rounded-full border-[1.5px] border-muted",
  retired: "h-0.5 w-2 rounded-full bg-muted",
} as const;

function known(state: string): state is keyof typeof marks {
  return Object.hasOwn(marks, state);
}

/**
 * StateMark is a Host's state as a mark and its word (docs/web-design.md,
 * "Components"): Managed a filled green dot with a halo, Registered a hollow
 * dot, Retired a short muted bar. Colour never carries the state alone. A
 * state this web UI does not know shows as stored, with no mark.
 */
export function StateMark({ state }: { state: string }) {
  const t = useT();
  return (
    <span
      className={`inline-flex items-center gap-2 text-[0.8125rem] font-medium whitespace-nowrap ${state === "retired" ? "text-muted" : "text-ink-2"}`}
    >
      {known(state) && <span aria-hidden className={`shrink-0 ${marks[state]}`} />}
      {known(state) ? t(`host.state.${state}`) : state}
    </span>
  );
}
