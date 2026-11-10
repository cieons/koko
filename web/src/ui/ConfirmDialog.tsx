import { AlertDialog, Button, Input, Label, TextField } from "@heroui/react";
import { useId, useState } from "react";

import { useT } from "../shell/i18n";
import { Refusal } from "./states";

/** ConfirmDialogProps are what an irreversible action says before it runs. */
export interface ConfirmDialogProps {
  /** title names the action and what it acts on, such as "Revoke laptop". */
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
  /** pending says the action's call runs: Escape no longer cancels, and its button takes no second press. */
  pending?: boolean;
  /** error is why the action was refused, if it was, shown in place. */
  error?: unknown;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * ConfirmDialog asks before an irreversible action: retire, removing a
 * record, revoke and removing a User. Reversible writes, revealing a Secret
 * and issuing a Join Code never ask. It is HeroUI's AlertDialog, open while
 * the page renders it (role alertdialog, named by its title and described
 * by the consequence): the page behind takes no input, a click beside it
 * does nothing, Escape cancels unless the call runs, and the focus goes back
 * to what opened it when it closes. Its first control takes the focus:
 * Cancel, so Enter never acts by accident, or the field for the typed name,
 * and the action stays disabled until that name is typed exactly. The
 * refusal shows in place.
 */
export function ConfirmDialog({
  title,
  consequence,
  confirm,
  typedName,
  pending = false,
  error,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const t = useT();
  const consequenceId = useId();
  const [typed, setTyped] = useState("");
  const ready = typedName === undefined || typed === typedName;
  return (
    <AlertDialog.Backdrop
      isOpen
      isDismissable={false}
      isKeyboardDismissDisabled={pending}
      onOpenChange={(open) => open || onCancel()}
    >
      <AlertDialog.Container size="sm">
        <AlertDialog.Dialog aria-describedby={consequenceId}>
          <AlertDialog.Header>
            <AlertDialog.Icon status="danger" />
            <AlertDialog.Heading>{title}</AlertDialog.Heading>
          </AlertDialog.Header>
          <AlertDialog.Body className="flex flex-col gap-4">
            <p id={consequenceId} className="text-muted">
              {consequence}
            </p>
            {typedName !== undefined && (
              <TextField value={typed} onChange={setTyped} autoFocus>
                <Label>{t("shell.confirm.typeName", { name: typedName })}</Label>
                <Input className="t-code" autoComplete="off" spellCheck="false" autoCapitalize="none" />
              </TextField>
            )}
            <Refusal error={error} />
          </AlertDialog.Body>
          <AlertDialog.Footer>
            <Button variant="tertiary" autoFocus={typedName === undefined} onPress={onCancel}>
              {t("shell.cancel")}
            </Button>
            <Button variant="danger" isPending={pending} isDisabled={!ready} onPress={onConfirm}>
              {confirm}
            </Button>
          </AlertDialog.Footer>
        </AlertDialog.Dialog>
      </AlertDialog.Container>
    </AlertDialog.Backdrop>
  );
}
