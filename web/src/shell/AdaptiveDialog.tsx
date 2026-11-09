import { BottomSheet } from "@astryxdesign/core/BottomSheet";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { HStack, Layout, LayoutContent, LayoutFooter, Section, VStack } from "@astryxdesign/core/Layout";
import { Heading, Text } from "@astryxdesign/core/Text";
import type { ReactNode } from "react";

import { ErrorMessage, useT } from "./i18n";

// The dialogs pages open for an action that does not ask first: a form, a
// value shown, a step of a wizard. Each is an Astryx Dialog, or a bottom
// sheet on a phone (spec #108: "Phones"). A confirmation is ConfirmDialog.

/** phone is the width below which dialogs, detail panes and menus become bottom sheets: Astryx's md. */
export const phone = "(width < 768px)";

/** AdaptiveDialogProps are what an AdaptiveDialog shows. */
export interface AdaptiveDialogProps {
  /** title names the dialog, such as "Add Host". */
  title: string;
  /** description, when given, says under the title what the dialog is for, such as a wizard's step. */
  description?: string;
  /** onClose hears Escape, the close button, or a click beside an info dialog; the page then stops rendering it. */
  onClose: () => void;
  /**
   * isForm keeps a click beside it from closing it, so what was typed is
   * not lost; Escape and the close button still close it.
   */
  isForm?: boolean;
  children: ReactNode;
  /** footer holds the dialog's buttons, below its content. */
  footer?: ReactNode;
}

/**
 * AdaptiveDialog is open while the page renders it: an Astryx Dialog named
 * by its title, or a bottom sheet named so on a phone, with its content and,
 * below it, its footer. It takes the focus as it opens, and gives it back
 * where it was when it closes.
 */
export function AdaptiveDialog({ title, description, onClose, isForm = false, children, footer }: AdaptiveDialogProps) {
  const isPhone = useMediaQuery(phone);
  const close = (open: boolean) => open || onClose();
  const purpose = isForm ? "form" : "info";
  if (isPhone) {
    return (
      <BottomSheet isOpen onOpenChange={close} label={title} purpose={purpose} height="tall">
        <Section variant="transparent" padding={4}>
          <VStack gap={4}>
            <VStack gap={1}>
              <Heading level={2}>{title}</Heading>
              {description !== undefined && (
                <Text type="supporting" color="secondary">
                  {description}
                </Text>
              )}
            </VStack>
            {children}
            {footer}
          </VStack>
        </Section>
      </BottomSheet>
    );
  }
  return (
    <Dialog isOpen onOpenChange={close} purpose={purpose} width={480}>
      <Layout
        header={<DialogHeader title={title} subtitle={description} onOpenChange={close} />}
        content={
          <LayoutContent>
            <VStack gap={4}>{children}</VStack>
          </LayoutContent>
        }
        footer={footer && <LayoutFooter>{footer}</LayoutFooter>}
      />
    </Dialog>
  );
}

/** FormDialogProps are what a FormDialog shows and does. */
export interface FormDialogProps extends Omit<AdaptiveDialogProps, "isForm" | "footer"> {
  /** submit labels the button that acts, such as "Add". */
  submit: string;
  /** ready tells whether the form may be sent: its fields hold what the action needs, and no write is pending. */
  ready: boolean;
  /** error is why the action was refused, if it was, shown in place above the buttons. */
  error?: unknown;
  /** onSubmit sends the form; it runs only when ready, from the button or Enter in a field. */
  onSubmit: () => void;
}

/**
 * FormDialog is the form of one action that never asks first, in an
 * AdaptiveDialog: its fields, the refusal, if any, Cancel and the button
 * that acts, which sends only when the form is ready. Enter in a field
 * sends it too.
 */
export function FormDialog({ submit, ready, error, onSubmit, children, ...dialog }: FormDialogProps) {
  const t = useT();
  return (
    <AdaptiveDialog {...dialog} isForm>
      <form
        aria-label={dialog.title}
        onSubmit={(e) => {
          e.preventDefault();
          if (ready) {
            onSubmit();
          }
        }}
      >
        <VStack gap={4}>
          {children}
          {error !== undefined && <ErrorMessage error={error} />}
          <HStack gap={2} hAlign="end" wrap="wrap">
            <Button label={t("shell.dialog.cancel")} variant="ghost" onClick={dialog.onClose} />
            <Button label={submit} variant="primary" type="submit" isDisabled={!ready} />
          </HStack>
        </VStack>
      </form>
    </AdaptiveDialog>
  );
}
