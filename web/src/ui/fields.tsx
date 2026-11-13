import {
  Autocomplete,
  ComboBox,
  Description,
  FieldError,
  Input,
  Label,
  ListBox,
  SearchField,
  Select,
  Tag,
  TagGroup,
  TextArea,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  useFilter,
} from "@heroui/react";
import { type ReactNode, use, useId } from "react";
import { ComboBoxStateContext } from "react-aria-components";

import { useT } from "../shell/i18n";

// The fields of koko's forms (docs/web-design.md, "Components"): a label
// above, help under it, and the error in place. A form checks them when it
// is submitted (Form): validate returns koko's message for a value the
// field does not take, which shows under the field then.

/**
 * listSearchClass is the width of a list's search field in its toolbar: it
 * fills its row, at least 14rem wide, or as wide as the row where that is
 * narrower: a rem grows with the browser's text size, and at 200% on a
 * phone 14rem is wider than the screen (#130).
 */
export const listSearchClass = "min-w-[min(14rem,100%)] flex-1";

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
  /**
   * inlineLabel shows the label in the field before its value, in place of
   * above it, where the value alone does not say what it is, such as Group
   * by's key in the Hosts page's row of controls.
   */
  inlineLabel?: boolean;
  placeholder?: string;
  validate?: (value: string | null) => string | undefined;
  isDisabled?: boolean;
  className?: string;
}

/**
 * SearchChoice picks one item from a list that can grow, such as a Space,
 * a User, the Relay or an Attribute key: HeroUI's ComboBox, a field that
 * narrows the list as it is typed in, matching anywhere in an item's label
 * whatever its case (the Owner's rule, docs/web-design.md, "Searchable
 * choices"). It takes an item of its list alone, unless newValues.
 *
 * Its list opens as the field is typed in or pressed, or on its button or
 * the up or down arrow. Unlike HeroUI's default, it does not open as the field
 * merely takes the focus: a modal overlay gives the focus back to the field it was in when
 * a click beside the overlay takes it, and a list opening then would take
 * that click, so the overlay would never close (docs/web-design.md,
 * "Searchable choices").
 */
export function SearchChoice(props: SearchChoiceProps) {
  const { contains } = useFilter({ sensitivity: "base" });
  const { label, value, onChange, options, newValues = false, validate } = props;
  const labelAbove = props.hideLabel !== true && props.inlineLabel !== true;
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
      aria-label={labelAbove ? undefined : label}
      {...selection}
      validate={validate && (() => validate(value))}
      defaultFilter={contains}
      menuTrigger="input"
      isDisabled={props.isDisabled}
      className={props.className}
      fullWidth
    >
      {labelAbove && <Label>{label}</Label>}
      <ComboBox.InputGroup>
        {/* The field is named by its aria-label already, so the label shown in it is not read twice. */}
        {props.inlineLabel === true && (
          <span aria-hidden className="t-label shrink-0 ps-3 whitespace-nowrap">
            {label}
          </span>
        )}
        <SearchInput placeholder={props.placeholder} />
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

/** SearchInput is a SearchChoice's field, which opens the whole list when pressed. */
function SearchInput({ placeholder }: { placeholder?: string }) {
  const state = use(ComboBoxStateContext);
  return (
    <Input
      placeholder={placeholder}
      autoComplete="off"
      spellCheck="false"
      onClick={() => state?.open(null, "manual")}
    />
  );
}

/** SearchChoicesProps are a searchable choice of several items. */
export interface SearchChoicesProps {
  label: string;
  /** values are the ids of the items picked, in the order they were picked. */
  values: readonly string[];
  onChange: (values: string[]) => void;
  options: readonly Option[];
  /** placeholder is what the field says while nothing is picked. */
  placeholder: string;
  /** searchLabel names the search field over the list. */
  searchLabel: string;
  /** noMatch is what the list says when no item holds what is searched for. */
  noMatch: string;
  description?: ReactNode;
  /** required, when given, is the message under the field when the form is submitted with nothing picked. */
  required?: string;
  autoFocus?: boolean;
  className?: string;
}

/**
 * SearchChoices picks several items from a list that can grow, such as the
 * Users to make Members of a Space: HeroUI's Autocomplete, whose list is
 * narrowed by a search field over it, matching anywhere in an item's label
 * whatever its case, and ticks each item picked. The field shows the items
 * picked as tags, each with its remove button.
 */
export function SearchChoices(props: SearchChoicesProps) {
  const t = useT();
  const { contains } = useFilter({ sensitivity: "base" });
  const { label, values, onChange, options, required } = props;
  const labelOf = (id: string) => options.find((o) => o.id === id)?.label ?? id;
  return (
    <Autocomplete
      selectionMode="multiple"
      value={[...values]}
      onChange={(keys) => onChange(Array.isArray(keys) ? keys.map(String) : [])}
      isRequired={required !== undefined}
      placeholder={props.placeholder}
      autoFocus={props.autoFocus}
      className={props.className}
      fullWidth
    >
      <Label>{label}</Label>
      <Autocomplete.Trigger className="h-auto min-h-10 py-1.5">
        <Autocomplete.Value>
          {({ defaultChildren, isPlaceholder }) =>
            isPlaceholder || values.length === 0 ? (
              defaultChildren
            ) : (
              <TagGroup
                size="sm"
                aria-label={t("shell.choices.picked", { label })}
                onRemove={(keys) => onChange(values.filter((id) => !keys.has(id)))}
              >
                <TagGroup.List>
                  {values.map((id) => (
                    <Tag key={id} id={id} textValue={labelOf(id)}>
                      {labelOf(id)}
                      <Tag.RemoveButton aria-label={t("shell.choices.remove", { item: labelOf(id) })} />
                    </Tag>
                  ))}
                </TagGroup.List>
              </TagGroup>
            )
          }
        </Autocomplete.Value>
        <Autocomplete.Indicator />
      </Autocomplete.Trigger>
      {props.description !== undefined && <Description>{props.description}</Description>}
      <FieldError>{(v) => (v.validationDetails.valueMissing ? required : v.validationErrors.join(" "))}</FieldError>
      {/* The popover scrolls, within the height React Aria gives it, not
          its list: the list takes no focus of its own, the search field
          holding it, so a keyboard reaches what scrolls through the field,
          which stays in view. */}
      <Autocomplete.Popover className="overflow-y-auto">
        <Autocomplete.Filter filter={contains}>
          <SearchField
            autoFocus
            aria-label={props.searchLabel}
            variant="secondary"
            className="sticky top-0 z-10 bg-overlay px-1.5 pb-1"
          >
            <SearchField.Group>
              <SearchField.SearchIcon />
              <SearchField.Input placeholder={props.searchLabel} autoComplete="off" spellCheck="false" />
              <SearchField.ClearButton aria-label={t("shell.choices.clearSearch")} />
            </SearchField.Group>
          </SearchField>
          <ListBox
            className="max-h-none overflow-visible"
            renderEmptyState={() => <p className="px-3 py-2 text-sm text-muted">{props.noMatch}</p>}
          >
            {optionItems(options)}
          </ListBox>
        </Autocomplete.Filter>
      </Autocomplete.Popover>
    </Autocomplete>
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
