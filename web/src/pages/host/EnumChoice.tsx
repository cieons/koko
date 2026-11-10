import { Choice, SearchChoice } from "../../ui/fields";
import type { KeyType } from "./view";

/** manyValues is how many values an enum has before its value is picked in a searchable choice. */
const manyValues = 8;

/** EnumChoiceProps are an enum's value field's. */
export interface EnumChoiceProps {
  label: string;
  type: KeyType;
  /** value is the value picked, or "" for none. */
  value: string;
  onChange: (value: string) => void;
  validate?: (value: string) => string | undefined;
  className?: string;
}

/**
 * EnumChoice picks an enum's value, as stored, in its declared order: a
 * select for a short list, such as a billing cycle, and a searchable choice
 * for a long one, such as currency (docs/web-design.md, "Searchable
 * choices").
 */
export function EnumChoice({ label, type, value, onChange, validate, className }: EnumChoiceProps) {
  const options = (type.values ?? []).map((v) => ({ id: v, label: v }));
  const props = {
    label,
    value: value === "" ? null : value,
    options,
    validate: validate && ((v: string | null) => validate(v ?? "")),
    className,
  };
  return options.length > manyValues ? (
    <SearchChoice {...props} onChange={(v) => onChange(v ?? "")} />
  ) : (
    <Choice {...props} onChange={onChange} />
  );
}
