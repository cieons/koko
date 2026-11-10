import { Button, Modal } from "@heroui/react";
import { type ReactNode, useEffect, useRef } from "react";

import type { JoinCode } from "../../api/device";
import { useT } from "../../shell/i18n";
import { Form } from "../../ui/Form";
import { ShownOnceLine } from "../../ui/ShownOnceLine";
import { Refusal } from "../../ui/states";

/**
 * joinCodeValidFor is how long a Join Code the web UI issues stays valid, in
 * seconds: the koko server's 15 minutes, since the web UI asks for no other
 * (Valid for comes with #116).
 */
export const joinCodeValidFor = 15 * 60;

/** IssueDialogProps are what a dialog that issues a Join Code asks and shows. */
export interface IssueDialogProps {
  /** title names the first step, such as "Invite" or "Add device". */
  title: string;
  /** lead says under the title what is issued. */
  lead: string;
  /** submit labels the button that issues. */
  submit: string;
  /** pending says the issuing calls run: the button shows so and takes no second press. */
  pending: boolean;
  /** error is why the issue was refused, shown in place on the first step. */
  error?: unknown;
  onSubmit: () => void;
  /** onClose hears Cancel, Done, Escape and the close button; the page then forgets what was issued and stops rendering the dialog. */
  onClose: () => void;
  /** doneTitle names the second step, such as "bob is invited". */
  doneTitle: string;
  /** issued, once there is something to show once, is the second step's content. */
  issued?: ReactNode;
  /** children are the first step's fields. */
  children: ReactNode;
}

/**
 * IssueDialog issues a Join Code in two steps (docs/web-design.md,
 * "Invite"): the first takes what it is for and issues it, never asking
 * first; the second shows what was issued, once, with Done, which takes the
 * focus, since the button that issued goes with the first step. The page
 * keeps what was issued in useShownOnce and forgets it as the dialog closes.
 */
export function IssueDialog(props: IssueDialogProps) {
  const t = useT();
  const done = props.issued !== undefined;
  // The button that issued goes with the first step, so the focus moves to Done.
  const doneButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (done) {
      doneButton.current?.focus();
    }
  }, [done]);
  const title = done ? props.doneTitle : props.title;
  const head = (
    <>
      <Modal.CloseTrigger aria-label={t("shell.close")} />
      <Modal.Header>
        <Modal.Heading className="t-title">{title}</Modal.Heading>
        {!done && <p className="t-label mt-1">{props.lead}</p>}
      </Modal.Header>
    </>
  );
  return (
    <Modal.Backdrop isOpen isDismissable={false} onOpenChange={(open) => open || props.onClose()}>
      <Modal.Container size="md">
        <Modal.Dialog aria-label={title}>
          {done ? (
            <>
              {head}
              <Modal.Body className="flex flex-col gap-4 p-1">{props.issued}</Modal.Body>
              <Modal.Footer>
                <Button ref={doneButton} onPress={props.onClose}>
                  {t("device.done")}
                </Button>
              </Modal.Footer>
            </>
          ) : (
            <Form label={title} onSubmit={props.onSubmit}>
              {head}
              <Modal.Body className="flex flex-col gap-5 p-1">
                {props.children}
                <Refusal error={props.error} />
              </Modal.Body>
              <Modal.Footer>
                <Button variant="tertiary" onPress={props.onClose}>
                  {t("shell.cancel")}
                </Button>
                <Button type="submit" isPending={props.pending}>
                  {props.submit}
                </Button>
              </Modal.Footer>
            </Form>
          )}
        </Modal.Dialog>
      </Modal.Container>
    </Modal.Backdrop>
  );
}

/**
 * JoinCodeLine shows a Join Code just issued, once: the koko join line to
 * run on the new device, its time left and Copy (ShownOnceLine); expired
 * says what to do once its time has run out.
 */
export function JoinCodeLine({ joinCode, expired }: { joinCode: JoinCode; expired: string }) {
  const t = useT();
  return (
    <ShownOnceLine
      label={t("device.joinCode.label")}
      intro={t("device.joinCode.intro", { device: joinCode.device, user: joinCode.user })}
      line={`koko join ${joinCode.code}`}
      validFor={joinCodeValidFor}
      expired={expired}
    />
  );
}
