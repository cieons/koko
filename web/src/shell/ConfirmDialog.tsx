import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Button } from "@astryxdesign/core/Button";
import { Dialog } from "@astryxdesign/core/Dialog";
import { Heading } from "@astryxdesign/core/Heading";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { Layout, LayoutContent, LayoutFooter } from "@astryxdesign/core/Layout";
import { Stack, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useId, useState } from "react";

import { useT } from "./i18n";

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
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * ConfirmDialog asks before an irreversible action: retire, removing a record,
 * revoke and removing a User. Reversible writes, revealing a Secret and
 * issuing a Join Code never ask. It is Astryx's AlertDialog, open while the
 * page renders it: the page behind takes no input, a click outside does not
 * close it, Escape cancels, and the focus goes back where it was when it
 * closes. Its first control takes the focus: Cancel, so Enter never acts by
 * accident, or the field for the typed name. On a phone its buttons fill
 * the width, the action above Cancel.
 */
export function ConfirmDialog(props: ConfirmDialogProps) {
  const t = useT();
  if (props.typedName !== undefined) {
    return <TypedNameDialog {...props} typedName={props.typedName} />;
  }
  const { title, consequence, confirm, onConfirm, onCancel } = props;
  return (
    <AlertDialog
      isOpen
      onOpenChange={(open) => open || onCancel()}
      title={title}
      description={consequence}
      actionLabel={confirm}
      cancelLabel={t("shell.confirm.cancel")}
      onAction={onConfirm}
    />
  );
}

/** smallScreen is the width at and below which AlertDialog stacks its buttons. */
const smallScreen = "(max-width: 640px)";

/**
 * TypedNameDialog is AlertDialog's own composition (Dialog, Layout, Heading,
 * Text and Buttons, as an alertdialog named by its title and described by
 * its consequence) with the field for the name between the consequence and
 * the buttons, since AlertDialog takes no content of its own. The field
 * takes the focus; the action stays disabled until the name is typed exactly.
 */
function TypedNameDialog({
  title,
  consequence,
  confirm,
  typedName,
  onConfirm,
  onCancel,
}: ConfirmDialogProps & { typedName: string }) {
  const t = useT();
  const titleId = useId();
  const consequenceId = useId();
  const [typed, setTyped] = useState("");
  const isSmallScreen = useMediaQuery(smallScreen);
  const ready = typed === typedName;
  const cancel = <Button key="cancel" variant="ghost" label={t("shell.confirm.cancel")} onClick={onCancel} />;
  const act = <Button key="act" variant="destructive" label={confirm} isDisabled={!ready} onClick={onConfirm} />;
  return (
    <Dialog
      isOpen
      onOpenChange={(open) => open || onCancel()}
      purpose="form"
      role="alertdialog"
      aria-labelledby={titleId}
      aria-describedby={consequenceId}
    >
      <Layout
        content={
          <LayoutContent>
            <VStack gap={4}>
              <VStack gap={2}>
                <Heading level={2} id={titleId}>
                  {title}
                </Heading>
                <Text type="body" color="secondary" id={consequenceId}>
                  {consequence}
                </Text>
              </VStack>
              <TextInput
                label={t("shell.confirm.typeName", { name: typedName })}
                value={typed}
                onChange={setTyped}
                hasAutoFocus
                autoComplete="off"
                hasSpellCheck={false}
              />
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <Stack
              direction={isSmallScreen ? "vertical" : "horizontal"}
              gap={2}
              hAlign={isSmallScreen ? "stretch" : "end"}
            >
              {isSmallScreen ? [act, cancel] : [cancel, act]}
            </Stack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
