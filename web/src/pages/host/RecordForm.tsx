import { Banner } from "@astryxdesign/core/Banner";
import { Selector } from "@astryxdesign/core/Selector";
import { Spinner } from "@astryxdesign/core/Spinner";
import { Text } from "@astryxdesign/core/Text";
import { TextArea } from "@astryxdesign/core/TextArea";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";

import { AttrList } from "../../api/attr";
import { FormDialog } from "../../shell/AdaptiveDialog";
import { useRead } from "../../shell/data";
import { useEditForm } from "../../shell/form";
import { useT } from "../../shell/i18n";
import { browserTimeZone } from "./reldate";
import { keyTypes, readValue } from "./view";

// The forms of a Host's and a Service's actions, in the shell's FormDialog:
// their fields, and the edit form that sends only what changed.

/**
 * Field is a text field of a form: a record's key, labelled by the key as
 * stored and never translated, or a message such as "New name".
 */
export function Field({
  label,
  value,
  onChange,
  inputMode,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  inputMode?: "numeric" | "decimal";
}) {
  return (
    <TextInput
      label={label}
      value={value}
      onChange={onChange}
      inputMode={inputMode}
      autoComplete="off"
      autoCapitalize="off"
      hasAutoCorrect={false}
      hasSpellCheck={false}
    />
  );
}

/** SpaceField chooses a Space among spaces, by name. */
export function SpaceField({
  label,
  spaces,
  value,
  onChange,
}: {
  label: string;
  spaces: readonly string[];
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <Selector
      label={label}
      options={spaces.map((s) => ({ value: s, label: s }))}
      value={value === "" ? undefined : value}
      onChange={onChange}
      presentation="adaptive"
    />
  );
}

/**
 * OwnField is one of a record's own fields its edit form changes, as the
 * form holds it: a Host's address, say. One that may be cleared is cleared
 * when emptied; valid, when given, checks a value before it is sent.
 */
export interface OwnField {
  key: string;
  value: string;
  clearable: boolean;
  valid?: (value: string) => boolean;
  inputMode?: "numeric";
}

/**
 * Changes are what an edit form sends: the own fields changed, as written,
 * the Attributes set, in their keys' types, the Note when changed, and the
 * keys cleared ("note" for the Note).
 */
export interface Changes {
  fields: Record<string, string>;
  attributes: Record<string, string | number>;
  note?: string;
  unset: string[];
}

/**
 * EditRecord is the edit form of a record's own fields, Attributes and
 * Note, in a FormDialog. It sends only what differs from the record as the
 * form opened with it (useEditForm), so a field someone else changed
 * meanwhile is never overwritten; an emptied value is cleared, and one more
 * Attribute may be added. A value of an Attribute is checked against its
 * key's type first, an enum's chosen from its list, and a relative date,
 * such as +1y, is resolved in the browser's time zone. Saving never asks.
 */
export function EditRecord({
  title,
  own,
  attributes,
  note,
  error,
  pending,
  onSave,
  onClose,
}: {
  title: string;
  own: readonly OwnField[];
  attributes: Readonly<Record<string, string | number>>;
  note: string | undefined;
  error: unknown;
  pending: boolean;
  onSave: (changes: Changes) => void;
  onClose: () => void;
}) {
  const t = useT();
  const attrs = useRead(AttrList, {});
  const form = useEditForm<Record<string, string>>({
    ...Object.fromEntries(own.map((f) => [f.key, f.value])),
    ...Object.fromEntries(Object.entries(attributes).map(([k, v]) => [k, String(v)])),
    note: note ?? "",
  });
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const types = keyTypes({}, attrs.data?.attributes ?? []);
  const clock = { now: new Date(), timeZone: browserTimeZone() };

  const changes: Changes = { fields: {}, attributes: {}, unset: [] };
  const faults: string[] = [];
  const setAttribute = (key: string, value: string) => {
    const type = types(key);
    const read = type && readValue(type, value, clock);
    if (read === undefined) {
      faults.push(key);
    } else {
      changes.attributes[key] = read;
    }
  };
  for (const [key, value = ""] of Object.entries(form.changes)) {
    const field = own.find((f) => f.key === key);
    if (value === "") {
      if (field && !field.clearable) {
        faults.push(key);
      } else {
        changes.unset.push(key);
      }
    } else if (field) {
      if (field.valid && !field.valid(value)) {
        faults.push(key);
      } else {
        changes.fields[key] = value;
      }
    } else if (key === "note") {
      changes.note = value;
    } else {
      setAttribute(key, value);
    }
  }
  if (newKey !== "" || newValue !== "") {
    if (newKey === "" || Object.hasOwn(form.values, newKey)) {
      faults.push(newKey === "" ? t("host.form.newKey") : newKey);
    } else {
      setAttribute(newKey, newValue);
    }
  }
  const changed =
    Object.keys(changes.fields).length + Object.keys(changes.attributes).length + changes.unset.length > 0 ||
    changes.note !== undefined;

  return (
    <FormDialog
      title={title}
      submit={t("host.form.save")}
      ready={attrs.data !== undefined && changed && faults.length === 0 && !pending}
      error={error ?? attrs.error}
      onSubmit={() => onSave(changes)}
      onClose={onClose}
    >
      {attrs.data === undefined ? (
        attrs.error === undefined && <Spinner label={t("shell.loading")} />
      ) : (
        <>
          <Text type="supporting" color="secondary">
            {t("host.form.clearHint")}
          </Text>
          {own.map((f) => (
            <Field
              key={f.key}
              label={f.key}
              value={form.values[f.key] ?? ""}
              inputMode={f.inputMode}
              onChange={(v) => form.set(f.key, v)}
            />
          ))}
          {Object.keys(attributes)
            .sort()
            .map((key) => {
              const values = types(key)?.values;
              const value = form.values[key] ?? "";
              return values ? (
                <Selector
                  key={key}
                  label={key}
                  options={values.map((v) => ({ value: v, label: v }))}
                  value={value === "" ? null : value}
                  onChange={(v: string | null) => form.set(key, v ?? "")}
                  hasClear
                  presentation="adaptive"
                />
              ) : (
                <Field key={key} label={key} value={value} onChange={(v) => form.set(key, v)} />
              );
            })}
          <Field label={t("host.form.newKey")} value={newKey} onChange={setNewKey} />
          <Field label={t("host.form.newValue")} value={newValue} onChange={setNewValue} />
          <TextArea label={t("host.note")} value={form.values["note"] ?? ""} onChange={(v) => form.set("note", v)} rows={5} />
          {faults.length > 0 && <Banner status="error" role="alert" title={t("host.form.invalid", { keys: faults.join(", ") })} />}
        </>
      )}
    </FormDialog>
  );
}
