import { useState } from "react";

/** EditForm is an edit form's values and what changed in them. */
export interface EditForm<T extends Record<string, unknown>> {
  values: T;
  set: <K extends keyof T>(field: K, value: T[K]) => void;
  /**
   * changes holds the fields whose value differs from the form's baseline:
   * the only fields its write sends, so a field someone else changed
   * meanwhile is never overwritten.
   */
  changes: Partial<T>;
  /**
   * saved makes the values the baseline, once a write of the changes has
   * succeeded, so setting a field back to its old value is a change again.
   */
  saved: () => void;
}

/**
 * useEditForm holds an edit form's values, whose baseline is initial, the
 * record as it was when the form opened. Values are what form controls hold
 * (strings, numbers, booleans); a field set back to its baseline is no
 * longer changed.
 */
export function useEditForm<T extends Record<string, unknown>>(initial: T): EditForm<T> {
  const [baseline, setBaseline] = useState(initial);
  const [values, setValues] = useState(initial);
  const set = <K extends keyof T>(field: K, value: T[K]) => setValues((v) => ({ ...v, [field]: value }));
  const changes: Partial<T> = {};
  for (const field of Object.keys(values) as (keyof T)[]) {
    if (!Object.is(values[field], baseline[field])) {
      changes[field] = values[field];
    }
  }
  return { values, set, changes, saved: () => setBaseline(values) };
}
