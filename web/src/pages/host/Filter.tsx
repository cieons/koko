import { Button, Chip, CloseButton, Popover } from "@heroui/react";
import { ListFilter, X } from "lucide-react";
import { lazy, Suspense, useState } from "react";

import type { AttrKey } from "../../api/attr";
import type { MessageKey } from "../../i18n/catalogues";
import { type Translate, useT } from "../../shell/i18n";
import { Choice, Field, type Option, SearchChoice } from "../../ui/fields";
import { Form } from "../../ui/Form";
import { EnumChoice } from "./EnumChoice";
import {
  type Clock,
  type Fields,
  type FilterOp,
  type KeyType,
  type KeyTypes,
  opsFor,
  readTerm,
  readValue,
  takesValue,
  writeTerm,
} from "./view";

// Filter: a popover that builds one filter term, a key, a comparison and a
// value, written into the URL as the CLI writes it; and the chips that show
// the view's terms, each with a button that takes it away.

// The date field brings the calendar, so it loads when a date is first filtered on.
const DateValue = lazy(() => import("./DateValue").then((m) => ({ default: m.DateValue })));

/** FilterProps are what Filter builds a term from. */
export interface FilterProps {
  /** fields are the list's own fields, offered first. */
  fields: Fields;
  /** attrs are the Attribute keys attr.list answered: the Presets, then those in use. */
  attrs: readonly AttrKey[];
  types: KeyTypes;
  clock: Clock;
  /** onAdd adds the term built, written as the CLI takes it. */
  onAdd: (term: string) => void;
}

/**
 * Filter is the button that opens the popover where a term is built: the
 * field or Attribute key, picked in a searchable choice; the comparison its
 * type takes; and the value, a select for an enum, the date field for a
 * date, and text otherwise. A value the key does not take is refused in the
 * popover, naming the term, and nothing changes.
 */
export function Filter({ fields, attrs, types, clock, onAdd }: FilterProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [key, setKey] = useState<string | null>(null);
  const [op, setOp] = useState<FilterOp>("eq");
  const [value, setValue] = useState("");
  const type = key === null ? undefined : types(key);
  const keys = [...new Set([...Object.keys(fields), ...attrs.map((a) => a.key)])];
  const options: Option[] = keys.map((k) => ({ id: k, label: k }));
  const close = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setKey(null);
      setOp("eq");
      setValue("");
    }
  };
  const chooseKey = (next: string | null) => {
    setKey(next);
    setValue("");
    const nextType = next === null ? undefined : types(next);
    if (nextType && !opsFor(nextType.type).includes(op)) {
      setOp("eq");
    }
  };
  const validateValue = (written: string) => {
    if (key === null || type === undefined) {
      return undefined;
    }
    if (written === "") {
      return t("host.filter.valueRequired");
    }
    return readValue(type, written, clock) === undefined
      ? t("host.filter.refused", { term: writeTerm(key, op, written) })
      : undefined;
  };
  const submit = () => {
    if (key === null) {
      return;
    }
    onAdd(writeTerm(key, op, takesValue(op) ? value : undefined));
    close(false);
  };
  return (
    <Popover isOpen={open} onOpenChange={close}>
      <Button variant="secondary">
        <ListFilter aria-hidden className="size-4" />
        {t("host.filter.open")}
      </Button>
      <Popover.Content placement="bottom end" className="w-[min(22rem,calc(100vw-1.5rem))]">
        <Popover.Dialog aria-label={t("host.filter.title")} className="p-4">
          <Form onSubmit={submit} label={t("host.filter.title")} className="flex flex-col gap-4">
            <Popover.Heading className="t-heading">{t("host.filter.title")}</Popover.Heading>
            <SearchChoice
              label={t("host.filter.key")}
              value={key}
              onChange={chooseKey}
              options={options}
              validate={(chosen) => (chosen === null ? t("host.filter.keyRequired") : undefined)}
            />
            {type && (
              <Choice
                label={t("host.filter.op")}
                value={op}
                onChange={(next) => setOp(next as FilterOp)}
                options={opsFor(type.type).map((o) => ({ id: o, label: t(`host.filter.op.${o}`) }))}
              />
            )}
            {type && takesValue(op) && (
              <ValueField type={type} value={value} onChange={setValue} validate={validateValue} />
            )}
            <Button type="submit" className="self-start">
              {t("host.filter.add")}
            </Button>
          </Form>
        </Popover.Dialog>
      </Popover.Content>
    </Popover>
  );
}

/** ValueField is where a term's value is given, as its key's type takes it. */
function ValueField({
  type,
  value,
  onChange,
  validate,
}: {
  type: KeyType;
  value: string;
  onChange: (value: string) => void;
  validate: (value: string) => string | undefined;
}) {
  const t = useT();
  const label = t("host.filter.value");
  switch (type.type) {
    case "enum":
      return <EnumChoice label={label} type={type} value={value} onChange={onChange} validate={validate} />;
    case "date":
      return (
        <Suspense>
          <DateValue label={label} value={value} onChange={onChange} validate={validate} />
        </Suspense>
      );
    default:
      return (
        <Field
          label={label}
          value={value}
          onChange={onChange}
          validate={validate}
          code
          inputMode={type.type === "number" ? "decimal" : undefined}
        />
      );
  }
}

/** symbols are the comparisons a chip shows as a sign; has and lacks are words. */
const symbols: Partial<Record<FilterOp, string>> = { eq: "=", ne: "≠", lt: "<", le: "≤", gt: ">", ge: "≥" };

/**
 * termLabel is a term as its chip shows it: its key, the comparison and its
 * value as written, a relative date with the date it resolves to today
 * (expires_at ≤ +30d (2026-12-09)). A term that does not read shows as
 * written.
 */
export function termLabel(t: Translate, written: string, types: KeyTypes, clock: Clock): string {
  const term = readTerm(written);
  if (!term) {
    return written;
  }
  const symbol = symbols[term.op];
  if (symbol === undefined || term.value === undefined) {
    return `${term.key} ${t(`host.filter.op.${term.op}` as MessageKey)}`;
  }
  const resolved = types(term.key)?.type === "date" ? readValue({ type: "date" }, term.value, clock) : undefined;
  const value = resolved === undefined || resolved === term.value ? term.value : `${term.value} (${resolved})`;
  return `${term.key} ${symbol} ${value}`;
}

/**
 * TermChips are the view's terms that no select shows, each a chip with a
 * button, named for it, that takes it away.
 */
export function TermChips({
  terms,
  types,
  clock,
  onRemove,
}: {
  /** terms are the terms, as written, with their index in the view. */
  terms: readonly { written: string; index: number }[];
  types: KeyTypes;
  clock: Clock;
  onRemove: (index: number) => void;
}) {
  const t = useT();
  if (terms.length === 0) {
    return null;
  }
  return (
    <ul aria-label={t("host.filter.terms")} className="mb-4 flex flex-wrap items-center gap-2">
      {terms.map(({ written, index }) => {
        const label = termLabel(t, written, types, clock);
        return (
          <li key={`${index}:${written}`}>
            <Chip variant="secondary" size="lg" className="t-code gap-1.5">
              {label}
              <CloseButton
                aria-label={t("host.filter.remove", { term: label })}
                onPress={() => onRemove(index)}
                className="-mr-1 size-5"
              >
                <X aria-hidden className="size-3.5" />
              </CloseButton>
            </Chip>
          </li>
        );
      })}
    </ul>
  );
}
