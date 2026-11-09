import type {
  FilterValue,
  OperatorValue,
  PowerSearchConfig,
  PowerSearchField,
  PowerSearchFilter,
} from "@astryxdesign/core/PowerSearch";
import { TextInput } from "@astryxdesign/core/TextInput";

import type { AttrKey } from "../../api/attr";
import { type Translate, useT } from "../../shell/i18n";
import { browserTimeZone, resolveDate } from "./reldate";
import { type Fields, type FilterOp, type KeyType, type KeyTypes, opsFor, type Term, takesValue, writeTerm } from "./view";

// A list's filter as Astryx's PowerSearch shows it: each token is one of
// the view's terms, as the CLI writes it in the URL's filter=. A token's
// field is the term's key, its operator the term's comparison (eq, ne, lt,
// le, gt, ge, has, lacks), and its value the term's value in PowerSearch's
// form for the key's type: text a string, a number a float, an enum's
// value an enum, and a date the CLI's own text (2026-12-01, today, +30d),
// so a relative date stays relative and its token shows the date it
// resolves to beside it.

/**
 * filterConfig is the PowerSearch config of a list: each of its fields,
 * then each Attribute key attr.list answers, a Preset or one in use, then
 * each other key the view's terms name, every one offering only the
 * comparisons its type takes (opsFor), labelled as koko writes them.
 */
export function filterConfig(
  t: Translate,
  name: string,
  fields: Fields,
  attrs: readonly AttrKey[],
  types: KeyTypes,
  named: readonly string[],
): PowerSearchConfig {
  const field = (key: string, group: string): PowerSearchField[] => {
    const type = types(key);
    return type ? [{ key, label: key, group, operators: operators(t, type) }] : [];
  };
  const known = new Set([...Object.keys(fields), ...attrs.map((a) => a.key)]);
  return {
    name,
    fields: [
      ...Object.keys(fields).flatMap((key) => field(key, t("host.filter.fields"))),
      ...attrs.filter((a) => a.preset).flatMap((a) => field(a.key, t("host.filter.presets"))),
      ...attrs.filter((a) => !a.preset).flatMap((a) => field(a.key, t("host.filter.inUse"))),
      ...[...new Set(named)].filter((key) => !known.has(key)).flatMap((key) => field(key, t("host.filter.inUse"))),
    ],
  };
}

/** operators are the comparisons a key of type takes, each with the value PowerSearch edits for it. */
function operators(t: Translate, type: KeyType): PowerSearchField["operators"] {
  return opsFor(type.type).map((op) => ({ key: op, label: t(`host.filter.op.${op}`), value: operatorValue(type, op) }));
}

/** operatorValue is how PowerSearch edits the value a term of type with op compares with. */
function operatorValue(type: KeyType, op: FilterOp): OperatorValue {
  if (!takesValue(op)) {
    return { type: "empty" };
  }
  switch (type.type) {
    case "enum":
      return { type: "enum", values: (type.values ?? []).map((v) => ({ value: v, label: v })) };
    case "number":
      return type.min === undefined ? { type: "float" } : { type: "float", minValue: type.min };
    case "date":
      return { type: "custom", Editor: DateEditor, getString: shownDate };
    default:
      return { type: "string", isArbitraryStringAllowed: true };
  }
}

/** tokenOf is the token of a term whose key is of type: the term as PowerSearch holds it. */
export function tokenOf(term: Term, type: KeyType): PowerSearchFilter {
  return { field: term.key, operator: term.op, value: tokenValue(type, term.value) };
}

function tokenValue(type: KeyType, value: string | undefined): FilterValue {
  if (value === undefined) {
    return { type: "empty" };
  }
  switch (type.type) {
    case "enum":
      return { type: "enum", value };
    case "number":
      return { type: "float", value: Number(value) };
    case "date":
      return { type: "custom", value };
    default:
      return { type: "string", value };
  }
}

/** termOf is the term a token stands for, written as the CLI takes it. */
export function termOf(token: PowerSearchFilter): string {
  return writeTerm(token.field, token.operator as FilterOp, valueOf(token.value));
}

/** valueOf is a token's value as the CLI writes it, or undefined for a comparison that takes none. */
function valueOf(value: FilterValue): string | undefined {
  switch (value.type) {
    case "string":
    case "enum":
    case "custom":
      return value.value;
    case "float":
    case "integer":
      return String(value.value);
    default:
      return undefined;
  }
}

/** shownDate is a date term's value in its token: as written, with the date a relative one resolves to today. */
function shownDate(written: string): string {
  const resolved = resolveDate(written, new Date(), browserTimeZone());
  return resolved === undefined || resolved === written ? written : `${written} (${resolved})`;
}

/**
 * DateEditor takes a date term's value as the CLI writes one, absolute or
 * relative, and shows the date it resolves to today, or that it is none.
 */
function DateEditor({
  value,
  onChange,
  isDisabled,
}: {
  value: string | null;
  onChange: (value: string | null) => void;
  isDisabled?: boolean;
}) {
  const t = useT();
  const written = value ?? "";
  const resolved = written === "" ? undefined : resolveDate(written, new Date(), browserTimeZone());
  return (
    <TextInput
      label={t("host.filter.date")}
      description={t("host.filter.dateForms")}
      value={written}
      onChange={(next) => onChange(next)}
      isDisabled={isDisabled}
      hasAutoFocus
      autoComplete="off"
      hasSpellCheck={false}
      hasAutoCorrect={false}
      autoCapitalize="off"
      status={
        written === ""
          ? undefined
          : resolved === undefined
            ? { type: "error", message: t("host.filter.notADate", { value: written }) }
            : { type: "success", message: t("host.filter.resolvesTo", { date: resolved }) }
      }
    />
  );
}
