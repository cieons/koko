import { Drawer } from "@heroui/react";
import type { ReactNode } from "react";

import { useT } from "../shell/i18n";
import { Form } from "./Form";
import { usePhone } from "./phone";

/** SidePanelProps are what a side panel shows. */
export interface SidePanelProps {
  /** title names the panel, such as "Add Host" or a Service's name. */
  title: string;
  /** subtitle, when given, says under the title what the panel holds. */
  subtitle?: ReactNode;
  /** wide makes it 44rem wide on a desktop, for the Host form, rather than 32rem. */
  wide?: boolean;
  /**
   * onSubmit, when given, makes the panel a form that validates on submit
   * (Form), so a submit button in its footer sends it.
   */
  onSubmit?: () => void;
  /** onClose hears Escape, the close button and a click beside it; the page then stops rendering the panel. */
  onClose: () => void;
  /** footer holds the panel's buttons. */
  footer?: ReactNode;
  children: ReactNode;
}

/**
 * SidePanel is HeroUI's Drawer, open while the page renders it
 * (docs/web-design.md, "Components"): from the right on a desktop, inset
 * 12px with a 1.75rem radius, and from the bottom on a phone, with a handle,
 * at most 92% of the height. It takes the focus as it opens and gives it
 * back to what opened it when it closes. A form's panel is not closed by a
 * click beside it, so what was typed is not lost.
 */
export function SidePanel({ title, subtitle, wide = false, onSubmit, onClose, footer, children }: SidePanelProps) {
  const t = useT();
  const phone = usePhone();
  const size = wide ? "w-[min(44rem,calc(100vw-1.5rem))]" : "w-[min(32rem,calc(100vw-1.5rem))]";
  const body = (
    <>
      {phone && <Drawer.Handle />}
      <Drawer.CloseTrigger aria-label={t("shell.close")} />
      <Drawer.Header className="gap-1 pr-10">
        <Drawer.Heading className="t-title">{title}</Drawer.Heading>
        {subtitle !== undefined && <p className="t-label">{subtitle}</p>}
      </Drawer.Header>
      <Drawer.Body className="flex flex-col gap-6">{children}</Drawer.Body>
      {footer !== undefined && <Drawer.Footer>{footer}</Drawer.Footer>}
    </>
  );
  return (
    <Drawer.Backdrop
      isOpen
      isDismissable={onSubmit === undefined}
      onOpenChange={(open) => open || onClose()}
      variant="opaque"
    >
      <Drawer.Content placement={phone ? "bottom" : "right"}>
        <Drawer.Dialog
          aria-label={title}
          className={
            phone ? "max-h-[92dvh] rounded-t-[1.75rem]" : `m-3 h-[calc(100dvh-1.5rem)] max-w-none rounded-[1.75rem] ${size}`
          }
        >
          {onSubmit === undefined ? (
            body
          ) : (
            <Form label={title} onSubmit={onSubmit} className="flex min-h-0 flex-1 flex-col">
              {body}
            </Form>
          )}
        </Drawer.Dialog>
      </Drawer.Content>
    </Drawer.Backdrop>
  );
}
