import { Button, Calendar, Description, FieldError, Input, Label, Popover, TextField } from "@heroui/react";
import { parseDate } from "@internationalized/date";
import { CalendarDays } from "lucide-react";
import { useState } from "react";

import { useT } from "../../shell/i18n";
import { browserTimeZone, resolveDate } from "./reldate";

// A date as the CLI writes one: a calendar date picked in the calendar or
// typed as 2026-12-01, or a date relative to today, today or +30d, which a
// filter keeps as written and a form resolves before it sends it. A module
// of its own, loaded with what uses it, so the calendar is not in the first
// load (ADR 0046).

/** DateValueProps are a date field's. */
export interface DateValueProps {
  label: string;
  /** value is the date as written: YYYY-MM-DD, today or a relative date, or "" for none. */
  value: string;
  onChange: (value: string) => void;
  /** validate is koko's message for a value the field does not take, checked when its form is submitted. */
  validate?: (value: string) => string | undefined;
  autoFocus?: boolean;
  className?: string;
}

/**
 * DateValue is a date typed as the CLI writes one, in the code face, or
 * picked in the calendar beside it. Under it, a relative date says the date
 * it resolves to today in the browser's time zone, and any other value the
 * forms the field takes.
 */
export function DateValue({ label, value, onChange, validate, autoFocus, className }: DateValueProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const resolved = value === "" ? undefined : resolveDate(value, new Date(), browserTimeZone());
  const relative = resolved !== undefined && resolved !== value;
  return (
    <div className={`flex items-start gap-2 ${className ?? ""}`}>
      <TextField value={value} onChange={onChange} validate={validate} autoFocus={autoFocus} className="min-w-0 flex-1" fullWidth>
        <Label>{label}</Label>
        <Input
          className="t-code"
          autoComplete="off"
          spellCheck="false"
          autoCapitalize="none"
          autoCorrect="off"
        />
        <Description>{relative ? t("host.date.resolves", { date: resolved }) : t("host.date.forms")}</Description>
        <FieldError />
      </TextField>
      <Popover isOpen={open} onOpenChange={setOpen}>
        <Button isIconOnly variant="secondary" aria-label={t("shell.openCalendar")} className="mt-6 shrink-0">
          <CalendarDays aria-hidden className="size-4" />
        </Button>
        <Popover.Content placement="bottom end">
          <Popover.Dialog aria-label={label} className="p-3">
            <Calendar
              aria-label={label}
              value={resolved === undefined ? null : parseDate(resolved)}
              onChange={(date) => {
                onChange(date.toString());
                setOpen(false);
              }}
            >
              <Calendar.Header>
                <Calendar.YearPickerTrigger>
                  <Calendar.YearPickerTriggerHeading />
                  <Calendar.YearPickerTriggerIndicator />
                </Calendar.YearPickerTrigger>
                <Calendar.NavButton slot="previous" />
                <Calendar.NavButton slot="next" />
              </Calendar.Header>
              <Calendar.Grid>
                <Calendar.GridHeader>{(day) => <Calendar.HeaderCell>{day}</Calendar.HeaderCell>}</Calendar.GridHeader>
                <Calendar.GridBody>{(date) => <Calendar.Cell date={date} />}</Calendar.GridBody>
              </Calendar.Grid>
              <Calendar.YearPickerGrid>
                <Calendar.YearPickerGridBody>
                  {({ year }) => <Calendar.YearPickerCell year={year} />}
                </Calendar.YearPickerGridBody>
              </Calendar.YearPickerGrid>
            </Calendar>
          </Popover.Dialog>
        </Popover.Content>
      </Popover>
    </div>
  );
}
