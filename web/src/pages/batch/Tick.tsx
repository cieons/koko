import { Checkbox } from "@heroui/react";
import { type MouseEvent, useRef } from "react";

import type { Ticked } from "./selection";

/** TickProps are a checkbox that ticks one record, or every record of a group. */
export interface TickProps {
  /** label names it, such as "Select web1" or "Select the 14 Hosts of ops". */
  label: string;
  /** ticked is whether the record is ticked, or how many of the group's are. */
  ticked: boolean | Ticked;
  /** onChange hears the new state, and whether Shift was held, which ticks a range. */
  onChange: (on: boolean, shift: boolean) => void;
  className?: string;
}

/**
 * Tick is the checkbox of a row or of a group's heading in a Select mode
 * (#134): HeroUI's Checkbox with no label shown, in a target of 44 pixels
 * a finger can hit. A click on it stays its own, so a row that toggles on
 * a click anywhere does not toggle twice.
 */
export function Tick({ label, ticked, onChange, className = "" }: TickProps) {
  const shift = useRef(false);
  return (
    <span
      onClickCapture={(event: MouseEvent) => {
        shift.current = event.shiftKey;
      }}
      onClick={(event) => event.stopPropagation()}
      className={`relative z-10 inline-flex size-11 shrink-0 items-center justify-center ${className}`}
    >
      <Checkbox
        aria-label={label}
        isSelected={ticked === true || ticked === "all"}
        isIndeterminate={ticked === "some"}
        onChange={(on) => onChange(on, shift.current)}
      >
        <Checkbox.Content className="p-2.5">
          <Checkbox.Control>
            <Checkbox.Indicator />
          </Checkbox.Control>
        </Checkbox.Content>
      </Checkbox>
    </span>
  );
}
