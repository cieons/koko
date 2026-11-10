import { Button, Dropdown, Label, Separator } from "@heroui/react";
import { Ellipsis } from "lucide-react";

/** RowAction is one action of a row menu. */
export interface RowAction {
  id: string;
  label: string;
  onAction: () => void;
  /** danger marks an action that takes something away, such as Retire or Delete, set apart after the others. */
  danger?: boolean;
  isDisabled?: boolean;
}

/** RowMenuProps are what a row menu holds, and how its button looks. */
export interface RowMenuProps {
  /** label names the menu and its button, such as "More actions for web1". */
  label: string;
  actions: readonly RowAction[];
  /** variant is the button's: ghost in a row, secondary beside a page's other actions. */
  variant?: "ghost" | "secondary";
}

/**
 * RowMenu holds the actions a row or a page uses less (docs/web-design.md,
 * "Components"): HeroUI's Dropdown behind an icon button named label, such
 * as "More actions for web1", the danger actions last, after a separator.
 */
export function RowMenu({ label, actions, variant = "ghost" }: RowMenuProps) {
  const plain = actions.filter((a) => a.danger !== true);
  const danger = actions.filter((a) => a.danger === true);
  const item = (a: RowAction) => (
    <Dropdown.Item
      key={a.id}
      id={a.id}
      textValue={a.label}
      variant={a.danger === true ? "danger" : "default"}
      isDisabled={a.isDisabled}
      onAction={a.onAction}
    >
      <Label>{a.label}</Label>
    </Dropdown.Item>
  );
  return (
    <Dropdown>
      <Button variant={variant} isIconOnly className="rounded-full" aria-label={label}>
        <Ellipsis aria-hidden className="size-[1.125rem]" />
      </Button>
      <Dropdown.Popover placement="bottom end" className="min-w-48">
        <Dropdown.Menu aria-label={label}>
          {plain.map(item)}
          {plain.length > 0 && danger.length > 0 && <Separator />}
          {danger.map(item)}
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}
