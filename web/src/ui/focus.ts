import { type RefObject, useEffect, useRef } from "react";

/**
 * useFocusFallback keeps the focus in place when an action takes away the
 * control that had it, as Revoke does with its own row once the list is
 * read again. arm() is called as the action succeeds, or as it starts
 * where what changes comes with its answer; then, when
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

/** RowFocus is what useRowFocus gives a list whose rows an action takes away. */
export interface RowFocus<T extends HTMLElement> {
  /** row is the ref callback of the control in the row of key that takes the focus, such as its Take out. */
  row: (key: string) => (element: HTMLElement | null) => void;
  /** fallback is the ref of what holds the control the focus goes to once the list is empty, such as its add field. */
  fallback: RefObject<T | null>;
  /** arm is called as the action on the row of key, among the rows keys in order, starts. */
  arm: (keys: readonly string[], key: string) => void;
  /** disarm is called when the action fails, so the row stays and the focus with it. */
  disarm: () => void;
}

/**
 * useRowFocus keeps the focus in a list when an action takes a row away, as
 * Take out does once the koko server answers or the list is read again:
 * armed as the action starts, once changed changes and that row is gone, if
 * the focus has fallen to the page's body or to what holds the list, the
 * next row's control takes it, or the one before for the last row, or, once
 * the list is empty, the first control fallback holds.
 */
export function useRowFocus<T extends HTMLElement>(changed: unknown): RowFocus<T> {
  const rows = useRef(new Map<string, HTMLElement>());
  const fallback = useRef<T>(null);
  // armed is the row going and the row the focus goes to, undefined for fallback.
  const armed = useRef<{ going: string; next?: string }>(undefined);
  useEffect(() => {
    const now = armed.current;
    if (now === undefined || rows.current.has(now.going)) {
      return;
    }
    // Armed for this row alone, so a later change never takes the focus.
    armed.current = undefined;
    const holder = fallback.current;
    const target =
      (now.next === undefined ? undefined : rows.current.get(now.next)) ??
      holder?.querySelector<HTMLElement>("input:not([type=hidden]):not([tabindex='-1']), button:not([tabindex='-1'])") ??
      holder;
    const active = document.activeElement;
    if (target && (active === null || active === document.body || active.contains(target))) {
      target.focus();
    }
  }, [changed]);
  return {
    row: (key) => (element) => {
      if (element) {
        rows.current.set(key, element);
      } else {
        rows.current.delete(key);
      }
    },
    fallback,
    arm: (keys, key) => {
      const at = keys.indexOf(key);
      armed.current = { going: key, next: keys[at + 1] ?? keys[at - 1] };
    },
    disarm: () => {
      armed.current = undefined;
    },
  };
}
