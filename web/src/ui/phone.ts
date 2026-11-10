import { useSyncExternalStore } from "react";

/**
 * phoneQuery is a phone's width: under Tailwind's md, 768 pixels, where the
 * places move to the bar at the foot and side panels come up from the
 * bottom (docs/web-design.md, "The frame").
 */
export const phoneQuery = "(width < 768px)";

/** usePhone tells whether the viewport is a phone's, following it as it changes. */
export function usePhone(): boolean {
  return useSyncExternalStore(
    (changed) => {
      const query = window.matchMedia(phoneQuery);
      query.addEventListener("change", changed);
      return () => query.removeEventListener("change", changed);
    },
    () => window.matchMedia(phoneQuery).matches,
  );
}
