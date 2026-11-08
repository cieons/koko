import { useState } from "react";

import type { AttrKey } from "../../api/attr";
import { useT } from "../../shell/i18n";
import {
  type Clock,
  type Fields,
  type FilterOp,
  type KeyType,
  type KeyTypes,
  opsFor,
  readValue,
  takesValue,
  writeTerm,
} from "./view";

/**
 * FilterBuilder builds a filter term in three steps: a key, one of the
 * list's fields or an Attribute key attr.list answers, a Preset or any key
 * in use; a comparison its type offers; and a value of its type, an enum's
 * from its list and a date from a picker or written relative, such as +30d.
 * It adds the term as the CLI writes it, such as expires_at.le=+30d, once
 * its value is one of the key's type.
 */
export function FilterBuilder({
  fields,
  attrs,
  types,
  clock,
  onAdd,
}: {
  fields: Fields;
  attrs: readonly AttrKey[];
  /** types are the types of the keys of fields and attrs. */
  types: KeyTypes;
  clock: Clock;
  onAdd: (term: string) => void;
}) {
  const t = useT();
  const [key, setKey] = useState("");
  const [op, setOp] = useState<FilterOp>("eq");
  const [value, setValue] = useState("");
  const type = types(key);
  const valid = type !== undefined && (!takesValue(op) || readValue(type, value, clock) !== undefined);

  const chooseKey = (next: string) => {
    const nextType = types(next);
    setKey(next);
    setValue("");
    if (nextType && !opsFor(nextType.type).includes(op)) {
      setOp("eq");
    }
  };

  return (
    <form
      className="filter-builder"
      onSubmit={(e) => {
        e.preventDefault();
        if (valid) {
          onAdd(writeTerm(key, op, takesValue(op) ? value : undefined));
          setKey("");
          setOp("eq");
          setValue("");
        }
      }}
    >
      <label>
        {t("host.filter.key")}{" "}
        <select value={key} onChange={(e) => chooseKey(e.target.value)}>
          <option value="">{t("host.filter.chooseKey")}</option>
          <optgroup label={t("host.filter.fields")}>
            {Object.keys(fields).map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </optgroup>
          <optgroup label={t("host.filter.presets")}>
            {attrs
              .filter((a) => a.preset)
              .map((a) => (
                <option key={a.key} value={a.key}>
                  {a.key}
                </option>
              ))}
          </optgroup>
          <optgroup label={t("host.filter.inUse")}>
            {attrs
              .filter((a) => !a.preset)
              .map((a) => (
                <option key={a.key} value={a.key}>
                  {a.key}
                </option>
              ))}
          </optgroup>
        </select>
      </label>
      {type && (
        <>
          <label>
            {t("host.filter.op")}{" "}
            <select value={op} onChange={(e) => setOp(e.target.value as FilterOp)}>
              {opsFor(type.type).map((o) => (
                <option key={o} value={o}>
                  {t(`host.filter.op.${o}`)}
                </option>
              ))}
            </select>
          </label>
          {takesValue(op) && <ValueInput type={type} value={value} onChange={setValue} />}
          {takesValue(op) && value !== "" && !valid && (
            <span className="error">{t("host.filter.invalidValue", { value, key })}</span>
          )}
        </>
      )}
      <button type="submit" disabled={!valid}>
        {t("host.filter.add")}
      </button>
    </form>
  );
}

/**
 * ValueInput takes a value of type: an enum's from its list, a date from a
 * picker or written, absolute or relative, and anything else written.
 */
function ValueInput({ type, value, onChange }: { type: KeyType; value: string; onChange: (value: string) => void }) {
  const t = useT();
  if (type.type === "enum") {
    return (
      <label>
        {t("host.filter.value")}{" "}
        <select value={value} onChange={(e) => onChange(e.target.value)}>
          <option value="">{t("host.filter.chooseValue")}</option>
          {type.values?.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
      </label>
    );
  }
  if (type.type === "date") {
    return (
      <>
        <label>
          {t("host.filter.date")}{" "}
          <input
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder={t("host.filter.dateForms")}
            autoComplete="off"
          />
        </label>
        <label>
          {t("host.filter.pickDate")}{" "}
          <input
            type="date"
            value={/^\d{4}-\d{2}-\d{2}$/.test(value) ? value : ""}
            onChange={(e) => onChange(e.target.value)}
          />
        </label>
      </>
    );
  }
  return (
    <label>
      {t("host.filter.value")}{" "}
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        inputMode={type.type === "number" ? "decimal" : undefined}
        autoComplete="off"
      />
    </label>
  );
}
