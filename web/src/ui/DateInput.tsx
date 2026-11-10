import { Calendar, DateField, DatePicker, Description, FieldError, Label } from "@heroui/react";
import { type CalendarDate, parseDate } from "@internationalized/date";
import type { ReactNode } from "react";

import { useT } from "../shell/i18n";

// The date field with its calendar, a module of its own, imported only by
// the forms that use it, so the calendar loads with them and not with the
// first page (ADR 0046).

/** DateInputProps are a date field's: a calendar date as the API keeps one. */
export interface DateInputProps {
  label: string;
  /** value is the date as YYYY-MM-DD, or "" for none. */
  value: string;
  onChange: (value: string) => void;
  description?: ReactNode;
  className?: string;
}

/** calendarDate is value as a CalendarDate, or null for none, or for one that does not read as a date. */
function calendarDate(value: string): CalendarDate | null {
  try {
    return value === "" ? null : parseDate(value);
  } catch {
    return null;
  }
}

/**
 * DateInput is a calendar date, typed in its segments or picked in the
 * calendar: HeroUI's DatePicker, in the order of the language's dates.
 */
export function DateInput({ label, value, onChange, description, className }: DateInputProps) {
  const t = useT();
  return (
    <DatePicker
      value={calendarDate(value)}
      onChange={(date) => onChange(date === null ? "" : date.toString())}
      className={className}
    >
      <Label>{label}</Label>
      <DateField.Group fullWidth>
        <DateField.Input>{(segment) => <DateField.Segment segment={segment} />}</DateField.Input>
        <DateField.Suffix>
          <DatePicker.Trigger aria-label={t("shell.openCalendar")}>
            <DatePicker.TriggerIndicator />
          </DatePicker.Trigger>
        </DateField.Suffix>
      </DateField.Group>
      {description !== undefined && <Description>{description}</Description>}
      <FieldError />
      <DatePicker.Popover>
        <Calendar aria-label={label}>
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
      </DatePicker.Popover>
    </DatePicker>
  );
}
