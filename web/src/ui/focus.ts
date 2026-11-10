import { type RefObject, useEffect, useRef } from "react";

/**
 * useFocusFallback keeps the focus in place when an action takes away the
 * control that had it, as Revoke or Take out does with its own row once the
 * list is read again. arm() is called as the action succeeds; then, when
 * changed (the list read again) next changes, if the focus has fallen to
 * the page's body or to what holds the list, such as the dialog it is in,
 * the element of the returned ref, such as the list's heading with
 * tabIndex -1, takes it.
 */
export function useFocusFallback<T extends HTMLElement>(changed: unknown): [RefObject<T | null>, () => void] {
  const ref = useRef<T>(null);
  const armed = useRef(false);
  useEffect(() => {
    if (!armed.current) {
      return;
    }
    // Armed for the next change alone, so a later one never takes the focus.
    armed.current = false;
    // Lost: on the body, or caught by what holds the list, as a dialog
    // catches the focus of a control taken from inside it.
    const now = document.activeElement;
    const target = ref.current;
    if (target && (now === null || now === document.body || now.contains(target))) {
      target.focus();
    }
  }, [changed]);
  return [
    ref,
    () => {
      armed.current = true;
    },
  ];
}
