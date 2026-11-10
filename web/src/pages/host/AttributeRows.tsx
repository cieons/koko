import { Button } from "@heroui/react";
import { Plus, Trash2 } from "lucide-react";

import type { AttrKey } from "../../api/attr";
import { useT } from "../../shell/i18n";
import { Field, SearchChoice } from "../../ui/fields";
import { DateValue } from "./DateValue";
import { EnumChoice } from "./EnumChoice";
import { attrType, type Clock, type KeyType, readValue, validAttrKey } from "./view";

// What the Host form and a Service's panel share of editing a record's
// Attributes and Note: the rows of key and value, a record's Attributes as
// rows, and what an edit sends of them.

/** AttributeRow is one row of the form's other Attributes: a key and its value, as typed. */
export interface AttributeRow {
  key: string;
  value: string;
}

/** typeOf is the type attr.list gives key, or text. */
export function typeOf(attrs: readonly AttrKey[], key: string): KeyType {
  const found = attrs.find((a) => a.key === key);
  return found ? attrType(found) : { type: "text" };
}

/**
 * AttributeRows are a form's Attributes of any key, a key and a value a
 * row, as the Host form and a Service's panel give them: the key picked
 * among those attr.list answers or typed anew, the value as its key's type
 * takes it. A key reserved, as the Host form's purchase fields are, or
 * given in another row is refused.
 */
export function AttributeRows({
  rows,
  onChange,
  reserved = [],
  attrs,
  clock,
}: {
  rows: readonly AttributeRow[];
  onChange: (rows: AttributeRow[]) => void;
  reserved?: readonly string[];
  attrs: readonly AttrKey[];
  clock: Clock;
}) {
  const t = useT();
  const setRow = (i: number, row: AttributeRow) => onChange(rows.map((r, j) => (j === i ? row : r)));
  const taken = (i: number) => new Set<string>([...reserved, ...rows.filter((_, j) => j !== i).map((r) => r.key)]);
  return (
    <>
      {rows.map((row, i) => {
        const type = typeOf(attrs, row.key);
        const keyOptions = attrs
          .filter((a) => !taken(i).has(a.key))
          .map((a) => ({ id: a.key, label: a.key }));
        const validateKey = (key: string | null) =>
          key === null
            ? row.value === ""
              ? undefined
              : t("host.form.required")
            : !validAttrKey(key)
              ? t("host.form.keyRule")
              : taken(i).has(key)
                ? t("host.form.keyTaken", { key })
                : undefined;
        const validateValue = (value: string) =>
          row.key === ""
            ? undefined
            : value.trim() === ""
              ? t("host.form.required")
              : readValue(type, value.trim(), clock) === undefined
                ? type.type === "date"
                  ? t("host.date.invalid", { value })
                  : t("host.form.invalid", { key: row.key })
                : undefined;
        const valueLabel = row.key === "" ? t("host.form.attrValueNew") : t("host.form.attrValue", { key: row.key });
        const setValue = (value: string) => setRow(i, { ...row, value });
        return (
          // A row has no id of its own, and goes only by its own button, so its index keys it.
          <div key={i} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-start gap-3">
            <SearchChoice
              label={t("host.form.attrKey")}
              value={row.key === "" ? null : row.key}
              onChange={(key) => setRow(i, { key: key ?? "", value: "" })}
              options={keyOptions}
              newValues
              validate={validateKey}
            />
            {type.type === "enum" ? (
              <EnumChoice label={valueLabel} type={type} value={row.value} onChange={setValue} validate={validateValue} />
            ) : type.type === "date" ? (
              <DateValue label={valueLabel} value={row.value} onChange={setValue} validate={validateValue} />
            ) : (
              <Field
                label={valueLabel}
                value={row.value}
                onChange={setValue}
                validate={validateValue}
                inputMode={type.type === "number" ? "decimal" : undefined}
              />
            )}
            <Button
              isIconOnly
              variant="tertiary"
              aria-label={row.key === "" ? t("host.form.attrRemoveNew") : t("host.form.attrRemove", { key: row.key })}
              onPress={() => onChange(rows.filter((_, j) => j !== i))}
              className="mt-6"
            >
              <Trash2 aria-hidden className="size-4" />
            </Button>
          </div>
        );
      })}
      <div>
        <Button variant="secondary" onPress={() => onChange([...rows, { key: "", value: "" }])}>
          <Plus aria-hidden className="size-4" />
          {t("host.form.attrAdd")}
        </Button>
      </div>
    </>
  );
}

/** text is an Attribute's value as a form's field holds it: a number as written, anything else as stored. */
export function text(value: string | number | undefined): string {
  return value === undefined ? "" : String(value);
}

/** rowsOf are a record's Attributes but those reserved, as rows of key and value, by key. */
export function rowsOf(attributes: Readonly<Record<string, string | number>>, reserved: readonly string[] = []): AttributeRow[] {
  return Object.keys(attributes)
    .filter((key) => !reserved.includes(key))
    .sort()
    .map((key) => ({ key, value: text(attributes[key]) }));
}

/**
 * RecordText is what an edit form holds of a record's Attributes and Note,
 * as typed: each Attribute by key, and the Note.
 */
export interface RecordText {
  attributes: ReadonlyMap<string, string>;
  note: string;
}

/** RecordChanges are what an edit sends of a record's Attributes and Note. */
export interface RecordChanges {
  attributes?: Record<string, string | number>;
  note?: string;
  /** unset are the Attribute keys emptied, and note when the Note was. */
  unset: string[];
}

/**
 * recordChanges are what an edit sends of after's Attributes and Note: the
 * Attributes that differ from before, the record as the form opened with
 * it, each as its key's type takes it in the API, a relative date resolved
 * in the browser's time zone as the CLI resolves one in a write; the Note
 * when it changed; and unset for each Attribute and the Note emptied.
 */
export function recordChanges(before: RecordText, after: RecordText, attrs: readonly AttrKey[], clock: Clock): RecordChanges {
  const changes: RecordChanges = { unset: [] };
  const attributes: Record<string, string | number> = {};
  for (const [key, typed] of after.attributes) {
    const value = typed.trim();
    const read = key === "" || value === "" ? undefined : readValue(typeOf(attrs, key), value, clock);
    if (read !== undefined && value !== before.attributes.get(key)?.trim()) {
      attributes[key] = read;
    }
  }
  for (const [key, typed] of before.attributes) {
    if (typed.trim() !== "" && (after.attributes.get(key) ?? "").trim() === "") {
      changes.unset.push(key);
    }
  }
  if (Object.keys(attributes).length > 0) {
    changes.attributes = attributes;
  }
  if (after.note.trim() === "") {
    if (before.note.trim() !== "") {
      changes.unset.push("note");
    }
  } else if (after.note !== before.note) {
    changes.note = after.note;
  }
  return changes;
}
