import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Step, Stepper } from "@astryxdesign/core/Stepper";
import { Text } from "@astryxdesign/core/Text";
import { type ReactNode, useEffect, useRef } from "react";

import type { JoinCode } from "../../api/device";
import { AdaptiveDialog } from "../../shell/AdaptiveDialog";
import { ErrorMessage, useT } from "../../shell/i18n";
import { JoinCodeLine } from "./JoinCodeLine";

/** Issued is what an Invite or an added device gave, shown once on the wizard's last step. */
export interface Issued {
  /** joinCode is the Join Code issued, if one was. */
  joinCode?: JoinCode;
  /** said is what the page says of what it did, such as whom it invited. */
  said?: string;
}

/** IssueWizardProps are what the wizard of an Invite or an added device takes. */
export interface IssueWizardProps {
  /** title names the flow, such as "Invite User". */
  title: string;
  /** step labels the first step, whose fields are children, such as "User". */
  step: string;
  /** submit labels the button that issues, such as "Invite". */
  submit: string;
  /** ready tells whether the first step may be sent. */
  ready: boolean;
  /** error is why the last request was refused, if it was. */
  error: unknown;
  /** issued is what the koko server answered, once it did; the wizard then shows its last step. */
  issued: Issued | undefined;
  onSubmit: () => void;
  /** onClose closes the wizard, forgetting what it issued. */
  onClose: () => void;
  children: ReactNode;
}

/**
 * IssueWizard is the flow that issues a Join Code, an Invite or an added
 * device, as Astryx's form-wizard-dialog, in the shell's AdaptiveDialog (a
 * bottom sheet on a phone): a compact stepper, the first step's fields, then
 * the Join Code shown once (JoinCodeLine). Issuing never asks first. Closing
 * it, from either step, forgets the code, as leaving the page does; the
 * page keeps it in useShownOnce meanwhile.
 */
export function IssueWizard({
  title,
  step,
  submit,
  ready,
  error,
  issued,
  onSubmit,
  onClose,
  children,
}: IssueWizardProps) {
  const t = useT();
  const done = useRef<HTMLButtonElement>(null);
  const last = issued !== undefined;
  // The button that issued goes with the first step, so the focus moves to
  // the last step's own button rather than fall to the page.
  useEffect(() => {
    if (last) {
      done.current?.focus();
    }
  }, [last]);
  return (
    <AdaptiveDialog title={title} onClose={onClose} isForm>
      <form
        aria-label={title}
        onSubmit={(e) => {
          e.preventDefault();
          if (!last && ready) {
            onSubmit();
          }
        }}
      >
        <VStack gap={4}>
          <Stepper
            activeStep={last ? 1 : 0}
            orientation="horizontal"
            density="compact"
            label={t("device.wizard.progress")}
          >
            <Step step={0} label={step} />
            <Step step={1} label={t("device.joinCode.title")} />
          </Stepper>
          {issued ? (
            <VStack gap={4}>
              {issued.said && <Text type="body">{issued.said}</Text>}
              {issued.joinCode && <JoinCodeLine key={issued.joinCode.id} joinCode={issued.joinCode} />}
            </VStack>
          ) : (
            <>
              {children}
              {error !== undefined && <ErrorMessage error={error} />}
            </>
          )}
          <HStack gap={2} hAlign="end" wrap="wrap">
            {last ? (
              <Button ref={done} variant="primary" label={t("device.wizard.done")} onClick={onClose} />
            ) : (
              <>
                <Button variant="ghost" label={t("shell.dialog.cancel")} onClick={onClose} />
                <Button variant="primary" type="submit" label={submit} isDisabled={!ready} />
              </>
            )}
          </HStack>
        </VStack>
      </form>
    </AdaptiveDialog>
  );
}
