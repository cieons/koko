import { Button, Modal } from "@heroui/react";
import { type ReactNode, useEffect, useRef } from "react";

import type { JoinCode } from "../../api/device";
import type { PasswordLink } from "../../api/signin";
import { useT } from "../../shell/i18n";
import { Segments } from "../../ui/fields";
import { Form } from "../../ui/Form";
import { ShownOnceLine } from "../../ui/ShownOnceLine";
import { Refusal } from "../../ui/states";

/**
 * validFors are the choices of Valid for, in seconds: 15 minutes, 1 hour, 1
 * day and 7 days, within the koko server's 60 to 604,800 (api.Validity).
 */
export const validFors = [900, 3600, 86_400, 604_800] as const;

/** ValidFor is one of the choices of Valid for, in seconds. */
export type ValidFor = (typeof validFors)[number];

/**
 * defaultValidFor is the koko server's own validity, 15 minutes (#108, ADR
 * 0044), which every Valid for starts at and a request leaves out.
 */
export const defaultValidFor: ValidFor = 900;

/**
 * validForRequest is the valid_for of a request that issues a Join Code or
 * Password Link: left out for the koko server's own 15 minutes, so such a
 * request is the one the web UI sent before it offered a choice.
 */
export function validForRequest(validFor: ValidFor): { valid_for?: number } {
  return validFor === defaultValidFor ? {} : { valid_for: validFor };
}

/** ValidForChoice is Valid for, a choice among its four, all in view (docs/web-design.md, "Invite"). */
export function ValidForChoice({ value, onChange }: { value: ValidFor; onChange: (value: ValidFor) => void }) {
  const t = useT();
  return (
    <Segments
      label={t("device.validFor")}
      value={String(value)}
      onChange={(id) => onChange(Number(id) as ValidFor)}
      options={validFors.map((v) => ({ id: String(v), label: t(`device.validFor.${v}`) }))}
    />
  );
}

/** IssueDialogProps are what a dialog that issues a Join Code or a Password Link asks and shows. */
export interface IssueDialogProps {
  /** title names the first step, such as "Invite" or "Add device". */
  title: string;
  /** lead says under the title what the screen cannot, such as an earlier link that stops working. */
  lead?: string;
  /** submit labels the button that issues. */
  submit: string;
  /** pending says the issuing calls run: the button shows so and takes no second press. */
  pending: boolean;
  /**
   * changed says a field of the first step holds something other than what
   * it opened with, typed or chosen: a click beside the dialog then no
   * longer closes it.
   */
  changed: boolean;
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
 * IssueDialog issues a Join Code or a Password Link in two steps
 * (docs/web-design.md, "Invite"): the first takes what it is for and issues it, never asking
 * first; the second shows what was issued, once, with Done, which takes the
 * focus, since the button that issued goes with the first step. The page
 * keeps what was issued in useShownOnce and forgets it as the dialog closes.
 * A click beside the first step closes it while nothing in it has changed
 * and no call runs; a click beside the second never does, since what it
 * shows cannot be shown again: Done, the close button and Escape close it
 * (docs/web-design.md, "Closing an overlay").
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
        {!done && props.lead !== undefined && <p className="t-label mt-1">{props.lead}</p>}
      </Modal.Header>
    </>
  );
  return (
    <Modal.Backdrop
      isOpen
      isDismissable={!done && !props.changed && !props.pending}
      onOpenChange={(open) => open || props.onClose()}
    >
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
 * run on the new device, its time left of validFor and Copy
 * (ShownOnceLine); expired says what to do once its time has run out.
 */
export function JoinCodeLine({
  joinCode,
  validFor,
  expired,
}: {
  joinCode: JoinCode;
  validFor: ValidFor;
  expired: string;
}) {
  const t = useT();
  return (
    <ShownOnceLine
      label={t("device.joinCode.label")}
      intro={t("device.joinCode.intro", { device: joinCode.device, user: joinCode.user })}
      line={`koko join ${joinCode.code}`}
      validFor={validFor}
      expired={expired}
    />
  );
}

/**
 * PasswordLinkLine shows a Password Link just issued, once: the link to send
 * to its User, its time left of validFor and Copy (ShownOnceLine); expired
 * says what to do once its time has run out.
 */
export function PasswordLinkLine({
  link,
  validFor,
  expired,
}: {
  link: PasswordLink;
  validFor: ValidFor;
  expired: string;
}) {
  const t = useT();
  return (
    <ShownOnceLine
      label={t("signin.link.label")}
      intro={t("signin.link.intro", { name: link.user })}
      line={link.link}
      validFor={validFor}
      expired={expired}
    />
  );
}
