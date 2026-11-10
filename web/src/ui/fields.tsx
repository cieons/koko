import {
  ComboBox,
  Description,
  FieldError,
  Input,
  Label,
  ListBox,
  Select,
  TextArea,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  useFilter,
} from "@heroui/react";
import { type ReactNode, useId } from "react";

// The fields of koko's forms (docs/web-design.md, "Components"): a label
// above, help under it, and the error in place. A form checks them when it
// is submitted (Form): validate returns koko's message for a value the
// field does not take, which shows under the field then.

/** Validate is a field's check: koko's message for a value it does not take, or undefined. */
export type Validate = (value: string) => string | undefined;

/** Option is one item of a list to pick from. */
export interface Option {
  /** id is the value the item stands for, such as a Space's name. */
  id: string;
  /** label is what the list shows; a name or value is shown as stored. */
  label: string;
  /** description, when given, says more under the label. */
  description?: string;
}

/** optionItems are a list's items, each its label with its description under it, for a Select or a ComboBox. */
function optionItems(options: readonly Option[]) {
  return options.map((o) => (
    <ListBox.Item key={o.id} id={o.id} textValue={o.label}>
      <Label>{o.label}</Label>
      {o.description !== undefined && <Description>{o.description}</Description>}
      <ListBox.ItemIndicator />
    </ListBox.Item>
  ));
}

/** FieldProps are a text field's. */
export interface FieldProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
  /** description is the help under the field. */
  description?: ReactNode;
  validate?: Validate;
  /** code sets the field in the code face, for a machine value: an address, a key, a Secret's value. */
  code?: boolean;
  /** multiline makes it a text area of rows lines. */
  multiline?: boolean;
  rows?: number;
  type?: "text" | "password" | "url";
  autoComplete?: string;
  inputMode?: "numeric" | "decimal" | "text";
  placeholder?: string;
  autoFocus?: boolean;
  isDisabled?: boolean;
  className?: string;
}

/** Field is a text field: HeroUI's TextField with its label, help and error. */
export function Field(props: FieldProps) {
  const code = props.code === true;
  const machine = code
    ? ({ className: "t-code", spellCheck: "false", autoCapitalize: "none", autoCorrect: "off" } as const)
    : {};
  return (
    <TextField
      value={props.value}
      onChange={props.onChange}
      validate={props.validate}
      isDisabled={props.isDisabled}
      autoFocus={props.autoFocus}
      type={props.type}
      className={props.className}
      fullWidth
    >
      <Label>{props.label}</Label>
      {props.multiline === true ? (
        <TextArea rows={props.rows ?? 4} placeholder={props.placeholder} {...machine} />
      ) : (
        <Input
          placeholder={props.placeholder}
          autoComplete={props.autoComplete ?? "off"}
          inputMode={props.inputMode}
          {...machine}
        />
      )}
      {props.description !== undefined && <Description>{props.description}</Description>}
      <FieldError />
    </TextField>
  );
}

/** ChoiceProps are a select's: one item of a short fixed list. */
export interface ChoiceProps {
  label: string;
  value: string | null;
  onChange: (id: string) => void;
  options: readonly Option[];
  description?: ReactNode;
  /** hideLabel names the control without showing its label, where its value says what it is. */
  hideLabel?: boolean;
  validate?: (id: string | null) => string | undefined;
  className?: string;
}

/**
 * Choice picks one item of a short fixed list, such as a comparison or a
 * billing cycle: HeroUI's Select. A list that can grow takes a SearchChoice.
 */
export function Choice({ label, value, onChange, options, description, hideLabel, validate, className }: ChoiceProps) {
  return (
    <Select
      aria-label={hideLabel === true ? label : undefined}
      value={value}
      onChange={(id) => id !== null && onChange(String(id))}
      validate={validate && ((id) => validate(id === null ? null : String(id)))}
      placeholder=" "
      className={className}
      fullWidth
    >
      {hideLabel !== true && <Label>{label}</Label>}
      <Select.Trigger>
        <Select.Value />
        <Select.Indicator />
      </Select.Trigger>
      {description !== undefined && <Description>{description}</Description>}
      <FieldError />
      <Select.Popover>
        <ListBox>
          {optionItems(options)}
        </ListBox>
      </Select.Popover>
    </Select>
  );
}

/** SearchChoiceProps are a searchable choice's. */
export interface SearchChoiceProps {
  label: string;
  /** value is the id of the item picked, or, with newValues, the text typed; null for none. */
  value: string | null;
  onChange: (value: string | null) => void;
  options: readonly Option[];
  /**
   * newValues lets the field take a value its list does not hold, as the
   * Attribute key of a form does: value is then the text typed, and the
   * list offers the items that match it.
   */
  newValues?: boolean;
  description?: ReactNode;
  hideLabel?: boolean;
  placeholder?: string;
  validate?: (value: string | null) => string | undefined;
  className?: string;
}

/**
 * SearchChoice picks one item from a list that can grow, such as a Space,
 * a User, the Relay or an Attribute key: HeroUI's ComboBox, a field that
 * narrows the list as it is typed in, matching anywhere in an item's label
 * whatever its case (the Owner's rule, docs/web-design.md, "Searchable
 * choices"). It takes an item of its list alone, unless newValues.
 */
export function SearchChoice(props: SearchChoiceProps) {
  const { contains } = useFilter({ sensitivity: "base" });
  const { label, value, onChange, options, newValues = false, validate } = props;
  const selection = newValues
    ? {
        allowsCustomValue: true,
        inputValue: value ?? "",
        onInputChange: (text: string) => onChange(text === "" ? null : text),
        value: options.some((o) => o.id === value) ? value : null,
        onChange: (id: unknown) => id !== null && onChange(String(id)),
      }
    : { value, onChange: (id: unknown) => onChange(id === null ? null : String(id)) };
  return (
    <ComboBox
      aria-label={props.hideLabel === true ? label : undefined}
      {...selection}
      validate={validate && (() => validate(value))}
      defaultFilter={contains}
      className={props.className}
      fullWidth
    >
      {props.hideLabel !== true && <Label>{label}</Label>}
      <ComboBox.InputGroup>
        <Input placeholder={props.placeholder} autoComplete="off" spellCheck="false" />
        <ComboBox.Trigger />
      </ComboBox.InputGroup>
      {props.description !== undefined && <Description>{props.description}</Description>}
      <FieldError />
      <ComboBox.Popover>
        <ListBox>
          {optionItems(options)}
        </ListBox>
      </ComboBox.Popover>
    </ComboBox>
  );
}

/** SegmentsProps are a choice among two to four, all in view. */
export interface SegmentsProps {
  label: string;
  value: string;
  onChange: (id: string) => void;
  options: readonly Option[];
}

/**
 * Segments is a choice among two to four, all in view, such as Valid for:
 * HeroUI's ToggleButtonGroup, whose chosen segment inverts.
 */
export function Segments({ label, value, onChange, options }: SegmentsProps) {
  const id = useId();
  return (
    <div className="flex flex-col gap-1.5">
      <span id={id} className="text-sm font-medium">
        {label}
      </span>
      <ToggleButtonGroup
        aria-labelledby={id}
        selectionMode="single"
        disallowEmptySelection
        selectedKeys={[value]}
        onSelectionChange={(keys) => {
          const [chosen] = [...keys];
          if (chosen !== undefined) {
            onChange(String(chosen));
          }
        }}
        fullWidth
      >
        {options.map((o, i) => (
          <ToggleButton key={o.id} id={o.id}>
            {i > 0 && <ToggleButtonGroup.Separator />}
            {o.label}
          </ToggleButton>
        ))}
      </ToggleButtonGroup>
    </div>
  );
}
