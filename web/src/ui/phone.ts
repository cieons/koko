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

/** coarseQuery is a touch screen as the primary pointer, which the terminal gives its key bar. */
export const coarseQuery = "(pointer: coarse)";

/**
 * sidewaysQuery is a touch screen held sideways at a phone's height, at
 * most 540 pixels, however wide: the terminal takes its phone layout there
 * too (docs/web-design.md, "The terminal").
 */
export const sidewaysQuery = "(pointer: coarse) and (orientation: landscape) and (max-height: 540px)";

/** useCoarsePointer tells whether the primary pointer is a touch screen, following it as it changes. */
export function useCoarsePointer(): boolean {
  return useMedia(coarseQuery);
}

/** useSideways tells whether the viewport is a phone's held sideways, following it as it changes. */
export function useSideways(): boolean {
  return useMedia(sidewaysQuery);
}
