import { useEffect, useId, useRef, useState } from "react";

import { useT } from "./i18n";

/** ConfirmDialogProps are what an irreversible action says before it runs. */
export interface ConfirmDialogProps {
  /** title asks the question, such as "Revoke laptop?". */
  title: string;
  /** consequence states what the action does that cannot be undone. */
  consequence: string;
  /** confirm labels the button that acts, such as "Revoke". */
  confirm: string;
  /**
   * typedName, when set, must be typed exactly before the action can run:
   * the web UI's form of the CLI's --last.
   */
  typedName?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * ConfirmDialog asks before an irreversible action: retire, removing a record,
 * revoke and removing a User. Reversible writes, revealing a Secret and
 * issuing a Join Code never ask. It is the browser's modal dialog, so the page
 * behind it takes no input, Escape cancels, and the focus goes back where it
 * was when it closes. Its first control takes the focus: Cancel, so Enter
 * never acts by accident, or the field for the typed name.
 */
export function ConfirmDialog({ title, consequence, confirm, typedName, onConfirm, onCancel }: ConfirmDialogProps) {
  const t = useT();
  const titleId = useId();
  const consequenceId = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const [typed, setTyped] = useState("");
  const ready = typedName === undefined || typed === typedName;

  useEffect(() => {
    const d = dialog.current;
    d?.showModal();
    return () => d?.close();
  }, []);

  return (
    <dialog
      ref={dialog}
      className="dialog"
      aria-labelledby={titleId}
      aria-describedby={consequenceId}
      onCancel={(e) => {
        e.preventDefault(); // the page closes it, by no longer rendering it
        onCancel();
      }}
    >
      <h2 id={titleId}>{title}</h2>
      <p id={consequenceId}>{consequence}</p>
      {typedName !== undefined && (
        <label>
          {t("shell.confirm.typeName", { name: typedName })}
          <input value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" spellCheck={false} />
        </label>
      )}
      <div className="dialog-actions">
        <button type="button" onClick={onCancel}>
          {t("shell.confirm.cancel")}
        </button>
        <button type="button" onClick={onConfirm} disabled={!ready}>
          {confirm}
        </button>
      </div>
    </dialog>
  );
}
