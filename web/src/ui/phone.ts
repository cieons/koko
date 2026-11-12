import { useSyncExternalStore } from "react";

/**
 * phoneQuery is a phone's width: under Tailwind's md, 768 pixels, where the
 * frame gives way to a top row and a bar at the foot and side panels come
 * up from the bottom (docs/web-design.md, "The frame").
 */
export const phoneQuery = "(width < 768px)";

/** useMedia tells whether the media query matches, following it as it changes. */
export function useMedia(media: string): boolean {
  return useSyncExternalStore(
    (changed) => {
      const query = window.matchMedia(media);
      query.addEventListener("change", changed);
      return () => query.removeEventListener("change", changed);
    },
    () => window.matchMedia(media).matches,
  );
}

/** usePhone tells whether the viewport is a phone's, following it as it changes. */
export function usePhone(): boolean {
  return useMedia(phoneQuery);
}
