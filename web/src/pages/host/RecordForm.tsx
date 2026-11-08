import { type ReactNode, useState } from "react";

import { AttrList } from "../../api/attr";
import { useRead } from "../../shell/data";
import { useEditForm } from "../../shell/form";
import { ErrorMessage, useT } from "../../shell/i18n";
import { browserTimeZone } from "./reldate";
import { keyTypes, readValue } from "./view";

// What a Host's page and a Service's page share: their Attributes and Note
// as stored, the form of a reversible action, and the edit form that sends
// only what changed.

/** Attributes shows a record's Attributes by key, as stored, never translated. */
export function Attributes({ attributes }: { attributes: Readonly<Record<string, string | number>> }) {
  const t = useT();
  const keys = Object.keys(attributes).sort();
  return (
    <>
      <h2>{t("host.attributes")}</h2>
      {keys.length === 0 ? (
        <p>{t("host.attributes.none")}</p>
      ) : (
        <table aria-label={t("host.attributes")}>
          <thead>
            <tr>
              <th>{t("host.attributes.key")}</th>
              <th>{t("host.attributes.value")}</th>
            </tr>
          </thead>
          <tbody>
            {keys.map((key) => (
              <tr key={key}>
                <td>{key}</td>
                <td>{attributes[key]}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}

/** Note shows a record's Note as stored, its lines kept. */
export function Note({ note }: { note: string | undefined }) {
  const t = useT();
  return (
    <>
      <h2>{t("host.note")}</h2>
      {note === undefined ? (
        <p>{t("host.note.none")}</p>
      ) : (
        <pre className="note" aria-label={t("host.note")}>
          {note}
        </pre>
      )}
    </>
  );
}

/**
 * ActionForm frames the form of one reversible action on a record: its
 * title, its fields, the button that acts, which never asks first, Cancel,
 * and the refusal, if any.
 */
export function ActionForm({
  title,
  submit,
  ready,
  error,
  onSubmit,
  onCancel,
  children,
}: {
  title: string;
  submit: string;
  ready: boolean;
  error: unknown;
  onSubmit: () => void;
  onCancel?: () => void;
  children: ReactNode;
}) {
  const t = useT();
  return (
    <section aria-label={title}>
      <h2>{title}</h2>
      <form
        className="record-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (ready) {
            onSubmit();
          }
        }}
      >
        {children}
        <div>
          <button type="submit" disabled={!ready}>
            {submit}
          </button>{" "}
          {onCancel && (
            <button type="button" onClick={onCancel}>
              {t("host.form.cancel")}
            </button>
          )}
        </div>
        {error !== undefined && <ErrorMessage error={error} />}
      </form>
    </section>
  );
}

/** Field is a field of a form, labelled by its key, as stored, never translated. */
export function Field({ name, value, onChange }: { name: string; value: string; onChange: (value: string) => void }) {
  return (
    <label>
      {name} <input value={value} onChange={(e) => onChange(e.target.value)} autoComplete="off" spellCheck={false} />
    </label>
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
 * Note. It sends only what differs from the record as the form opened with
 * it, so a field someone else changed meanwhile is never overwritten; an
 * emptied value is cleared, and one more Attribute may be added. A value of
 * an Attribute is checked against its key's type first, and a relative
 * date, such as +1y, is resolved in the browser's time zone. Saving never
 * asks.
 */
export function EditRecord({
  title,
  own,
  attributes,
  note,
  error,
  pending,
  onSave,
  onCancel,
}: {
  title: string;
  own: readonly OwnField[];
  attributes: Readonly<Record<string, string | number>>;
  note: string | undefined;
  error: unknown;
  pending: boolean;
  onSave: (changes: Changes) => void;
  onCancel: () => void;
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
  if (!attrs.data) {
    return attrs.error !== undefined ? <ErrorMessage error={attrs.error} /> : null;
  }
  const types = keyTypes({}, attrs.data.attributes);
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
    <ActionForm
      title={title}
      submit={t("host.form.save")}
      ready={changed && faults.length === 0 && !pending}
      error={error}
      onSubmit={() => onSave(changes)}
      onCancel={onCancel}
    >
      <p className="tag">{t("host.form.clearHint")}</p>
      {own.map((f) => (
        <Field key={f.key} name={f.key} value={form.values[f.key] ?? ""} onChange={(v) => form.set(f.key, v)} />
      ))}
      {Object.keys(attributes)
        .sort()
        .map((key) => {
          const values = types(key)?.values;
          const value = form.values[key] ?? "";
          return values ? (
            <label key={key}>
              {key}{" "}
              <select value={value} onChange={(e) => form.set(key, e.target.value)}>
                <option value="" />
                {values.map((v) => (
                  <option key={v} value={v}>
                    {v}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <Field key={key} name={key} value={value} onChange={(v) => form.set(key, v)} />
          );
        })}
      <label>
        {t("host.form.newKey")}{" "}
        <input value={newKey} onChange={(e) => setNewKey(e.target.value)} autoComplete="off" spellCheck={false} />
      </label>
      <label>
        {t("host.form.newValue")}{" "}
        <input value={newValue} onChange={(e) => setNewValue(e.target.value)} autoComplete="off" spellCheck={false} />
      </label>
      <label>
        {t("host.note")}
        <textarea value={form.values["note"] ?? ""} onChange={(e) => form.set("note", e.target.value)} />
      </label>
      {faults.length > 0 && (
        <p className="error" role="alert">
          {t("host.form.invalid", { keys: faults.join(", ") })}
        </p>
      )}
    </ActionForm>
  );
}
