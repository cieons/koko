import { Button, Modal } from "@heroui/react";
import type { ReactNode } from "react";

import { useT } from "../shell/i18n";
import { Form } from "./Form";
import { Refusal } from "./states";

/** FormDialogProps are what a short form shows and does. */
export interface FormDialogProps {
  /** title names the dialog and its form, such as "Rename web1". */
  title: string;
  /** lead, when given, says under the title what the form does. */
  lead?: ReactNode;
  /** submit labels the button that acts, such as "Rename". */
  submit: string;
  /** pending says the form's call runs: its button shows so and takes no second press. */
  pending: boolean;
  /** error is why the call was refused, if it was, shown in place above the buttons. */
  error?: unknown;
  /** onSubmit sends the form, once its fields are valid, from its button or Enter in a field. */
  onSubmit: () => void;
  /** onClose hears Cancel, Escape and the close button; the page then stops rendering the dialog. */
  onClose: () => void;
  children: ReactNode;
}

/**
 * FormDialog is a short form of one to three fields, such as a rename, a
 * move or a new value, in HeroUI's Modal, open while the page renders it
 * (docs/web-design.md, "Components"). It takes the focus as it opens and
 * gives it back to what opened it when it closes; a click beside it does
 * not close it, so what was typed is not lost. The form validates on
 * submit (Form), and a refusal shows in place, the form kept as typed.
 */
export function FormDialog({ title, lead, submit, pending, error, onSubmit, onClose, children }: FormDialogProps) {
  const t = useT();
  return (
    <Modal.Backdrop isOpen isDismissable={false} onOpenChange={(open) => open || onClose()}>
      <Modal.Container size="sm">
        <Modal.Dialog aria-label={title}>
          <Form label={title} onSubmit={onSubmit}>
            <Modal.CloseTrigger aria-label={t("shell.close")} />
            <Modal.Header>
              <Modal.Heading className="t-title">{title}</Modal.Heading>
              {lead !== undefined && <p className="t-label mt-1">{lead}</p>}
            </Modal.Header>
            <Modal.Body className="flex flex-col gap-4 p-1">
              {children}
              <Refusal error={error} />
            </Modal.Body>
            <Modal.Footer>
              <Button variant="tertiary" onPress={onClose}>
                {t("shell.cancel")}
              </Button>
              <Button type="submit" isPending={pending}>
                {submit}
              </Button>
            </Modal.Footer>
          </Form>
        </Modal.Dialog>
      </Modal.Container>
    </Modal.Backdrop>
  );
}
