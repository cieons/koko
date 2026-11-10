import { Button, Switch } from "@heroui/react";
import { Plus, Trash2 } from "lucide-react";
import { type ReactNode, useState } from "react";

import type { AttrKey } from "../../api/attr";
import { type Host, HostAdd, type NewHost } from "../../api/host";
import type { Space } from "../../api/space";
import { useCaller } from "../../shell/caller";
import { useWrite } from "../../shell/data";
import { useT } from "../../shell/i18n";
import { navigate } from "../../shell/router";
import { Field, SearchChoice } from "../../ui/fields";
import { notify } from "../../ui/notify";
import { SidePanel } from "../../ui/SidePanel";
import { Refusal } from "../../ui/states";
import { DateValue } from "./DateValue";
import { EnumChoice } from "./EnumChoice";
import { hostPath, validName, validPort } from "./record";
import { browserTimeZone } from "./reldate";
import { attrType, type Clock, type KeyType, readValue, validAttrKey } from "./view";

// The Host form (docs/web-design.md, "Add Host and Edit"): four groups, in
// the order of a provider's order confirmation. Add Host sends them in one
// host.add; Edit (#124) shows the same fields but Name and Space.

/** purchase are the Preset keys the purchase group gives a field each. */
const purchase = [
  "provider",
  "region",
  "price",
  "currency",
  "billing_cycle",
  "expires_at",
  "auto_renew",
  "provider_account",
] as const;

/** noRelay is the Relay choice's id for none: no Host's name, which never starts with '-'. */
const noRelay = "-";

/** AttributeRow is one row of the form's other Attributes: a key and its value, as typed. */
export interface AttributeRow {
  key: string;
  value: string;
}

/** HostValues are the form's values, as typed. */
export interface HostValues {
  name: string;
  space: string;
  address: string;
  account: string;
  port: string;
  /** relay is the Relay's name, or "" for none. */
  relay: string;
  provider: string;
  region: string;
  price: string;
  currency: string;
  billing_cycle: string;
  /** expires_at is the date as written: YYYY-MM-DD, today or a relative date, or "". */
  expires_at: string;
  auto_renew: boolean;
  provider_account: string;
  rows: AttributeRow[];
  note: string;
}

/** HostFieldsProps are what the form's fields are drawn from. */
export interface HostFieldsProps {
  values: HostValues;
  onChange: (values: HostValues) => void;
  /** spaces, when given, makes the form ask for the Name and Space, as Add Host does. */
  spaces?: readonly Space[];
  /** relays are the Hosts that may be a Relay: Managed, reached directly. */
  relays: readonly Host[];
  attrs: readonly AttrKey[];
  clock: Clock;
}

/** typeOf is the type attr.list gives key, or text. */
function typeOf(attrs: readonly AttrKey[], key: string): KeyType {
  const found = attrs.find((a) => a.key === key);
  return found ? attrType(found) : { type: "text" };
}

/**
 * HostFields are the Host form's four groups: the Host, how koko reaches
 * it, the purchase, and anything else. Each field checks its value when the
 * form is submitted, against its key's type as attr.list gives it.
 */
export function HostFields({ values, onChange, spaces, relays, attrs, clock }: HostFieldsProps) {
  const t = useT();
  const set = <K extends keyof HostValues>(key: K) => (value: HostValues[K]) => onChange({ ...values, [key]: value });
  // ofItsType is the check of a field against its key's type, as attr.list gives it.
  const ofItsType = (key: string) => (value: string) =>
    value === "" || readValue(typeOf(attrs, key), value.trim(), clock) !== undefined
      ? undefined
      : t("host.form.invalid", { key });
  return (
    <>
      <Group title={t("host.form.host")}>
        <div className="grid gap-4 sm:grid-cols-2">
          {spaces && (
            <>
              <Field
                label={t("host.key.name")}
                value={values.name}
                onChange={set("name")}
                description={t("host.form.nameHint")}
                validate={(v) => (validName(v) ? undefined : t("host.form.nameRule"))}
                code
                autoFocus
              />
              <SearchChoice
                label={t("host.key.space")}
                value={values.space}
                onChange={(v) => v !== null && set("space")(v)}
                description={t("host.form.spaceHint")}
                options={spaces.map((s) => ({
                  id: s.name,
                  label: s.name,
                  description: s.personal ? t("host.list.personal") : undefined,
                }))}
                validate={(v) => (v === null ? t("host.form.required") : undefined)}
              />
            </>
          )}
          <Field
            label={t("host.key.address")}
            value={values.address}
            onChange={set("address")}
            description={t("host.form.addressHint")}
            validate={(v) => (v.trim() === "" ? t("host.form.required") : undefined)}
            code
            className="sm:col-span-2"
          />
        </div>
      </Group>
      <Group title={t("host.form.reach")} lead={t("host.form.reachLead")}>
        <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
          <Field
            label={t("host.key.account")}
            value={values.account}
            onChange={set("account")}
            validate={(v) => (v.trim() === "" ? t("host.form.required") : undefined)}
            code
          />
          <Field
            label={t("host.key.port")}
            value={values.port}
            onChange={set("port")}
            validate={(v) => (validPort(v) ? undefined : t("host.form.portRule"))}
            inputMode="numeric"
            code
          />
          {relays.length > 0 && (
            <SearchChoice
              label={t("host.key.relay")}
              value={values.relay === "" ? noRelay : values.relay}
              onChange={(v) => set("relay")(v === null || v === noRelay ? "" : v)}
              description={t("host.form.relayHint")}
              options={[
                { id: noRelay, label: t("host.form.noRelay") },
                ...relays.map((r) => ({ id: r.name, label: r.name })),
              ]}
              className="sm:col-span-2"
            />
          )}
        </div>
      </Group>
      <Group title={t("host.form.purchase")} lead={t("host.form.purchaseLead")}>
        <div className="grid gap-4 sm:grid-cols-6">
          <Field
            label={t("host.key.provider")}
            value={values.provider}
            onChange={set("provider")}
            validate={ofItsType("provider")}
            className="sm:col-span-3"
          />
          <Field
            label={t("host.key.region")}
            value={values.region}
            onChange={set("region")}
            validate={ofItsType("region")}
            className="sm:col-span-3"
          />
          <Field
            label={t("host.key.price")}
            value={values.price}
            onChange={set("price")}
            validate={ofItsType("price")}
            inputMode="decimal"
            className="sm:col-span-2"
          />
          <EnumChoice
            label={t("host.key.currency")}
            type={typeOf(attrs, "currency")}
            value={values.currency}
            onChange={set("currency")}
            className="sm:col-span-2"
          />
          <EnumChoice
            label={t("host.key.billing_cycle")}
            type={typeOf(attrs, "billing_cycle")}
            value={values.billing_cycle}
            onChange={set("billing_cycle")}
            className="sm:col-span-2"
          />
          <DateValue
            label={t("host.key.expires_at")}
            value={values.expires_at}
            onChange={set("expires_at")}
            validate={(v) => (ofItsType("expires_at")(v) === undefined ? undefined : t("host.date.invalid", { value: v }))}
            className="sm:col-span-4"
          />
          <div className="flex items-center sm:col-span-2 sm:pt-6">
            <Switch isSelected={values.auto_renew} onChange={set("auto_renew")}>
              <Switch.Content>
                <Switch.Control>
                  <Switch.Thumb />
                </Switch.Control>
                {t("host.key.auto_renew")}
              </Switch.Content>
            </Switch>
          </div>
          <Field
            label={t("host.key.provider_account")}
            value={values.provider_account}
            onChange={set("provider_account")}
            description={t("host.form.providerAccountHint")}
            validate={ofItsType("provider_account")}
            className="sm:col-span-6"
          />
        </div>
      </Group>
      <Group title={t("host.form.more")} lead={t("host.form.moreLead")}>
        <AttributeRows rows={values.rows} onChange={set("rows")} attrs={attrs} clock={clock} />
        <Field
          label={t("host.key.note")}
          value={values.note}
          onChange={set("note")}
          description={t("host.form.noteHint")}
          multiline
          rows={4}
        />
      </Group>
    </>
  );
}

/** Group is one of the form's groups: a fieldset with its title and an optional line under it. */
function Group({ title, lead, children }: { title: string; lead?: string; children: ReactNode }) {
  return (
    <fieldset className="flex min-w-0 flex-col gap-4">
      <legend className="mb-4">
        <span className="t-heading block">{title}</span>
        {lead !== undefined && <span className="t-label mt-0.5 block">{lead}</span>}
      </legend>
      {children}
    </fieldset>
  );
}

/**
 * AttributeRows are the form's other Attributes, a key and a value a row:
 * the key picked among those attr.list answers or typed anew, the value as
 * its key's type takes it.
 */
function AttributeRows({
  rows,
  onChange,
  attrs,
  clock,
}: {
  rows: readonly AttributeRow[];
  onChange: (rows: AttributeRow[]) => void;
  attrs: readonly AttrKey[];
  clock: Clock;
}) {
  const t = useT();
  const setRow = (i: number, row: AttributeRow) => onChange(rows.map((r, j) => (j === i ? row : r)));
  const taken = (i: number) => new Set<string>([...purchase, ...rows.filter((_, j) => j !== i).map((r) => r.key)]);
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

/**
 * attributesOf are the Attributes the form gives, by key: each value given,
 * as its key's type takes it in the API, a relative date resolved in the
 * browser's time zone as the CLI resolves one in a write, and Renews
 * automatically as yes when it is on.
 */
export function attributesOf(values: HostValues, attrs: readonly AttrKey[], clock: Clock): Record<string, string | number> {
  const given: [string, string][] = [
    ...purchase.filter((k) => k !== "auto_renew").map((k): [string, string] => [k, values[k]]),
    ...values.rows.map((r): [string, string] => [r.key, r.value]),
  ];
  const attributes: Record<string, string | number> = {};
  for (const [key, value] of given) {
    const read = key === "" || value.trim() === "" ? undefined : readValue(typeOf(attrs, key), value.trim(), clock);
    if (read !== undefined) {
      attributes[key] = read;
    }
  }
  if (values.auto_renew) {
    attributes.auto_renew = "yes";
  }
  return attributes;
}

/** AddHostProps are what Add Host is drawn from, each undefined while its read loads. */
export interface AddHostProps {
  spaces: readonly Space[] | undefined;
  hosts: readonly Host[] | undefined;
  attrs: readonly AttrKey[] | undefined;
  onClose: () => void;
}

/**
 * AddHost records a Host (docs/web-design.md, "Add Host and Edit"), in a
 * wide side panel: one host.add with only the fields given, Attributes and
 * Note among them, in the caller's Personal Space unless they choose
 * another; then the page moves to the new Host.
 */
export function AddHost({ spaces, hosts, attrs = [], onClose }: AddHostProps) {
  const t = useT();
  const caller = useCaller();
  const add = useWrite(HostAdd);
  const personal = spaces?.find((s) => s.personal && s.name === caller.user.name)?.name ?? caller.user.name;
  const [values, setValues] = useState<HostValues>(() => ({
    ...blank,
    space: personal,
  }));
  const clock: Clock = { now: new Date(), timeZone: browserTimeZone() };
  const submit = () => {
    void add.submit({ hosts: [newHost(values, attrs, clock)] }).then((added) => {
      const [recorded] = added?.hosts ?? [];
      if (recorded) {
        notify(t("host.add.done", { name: recorded.name }));
        onClose();
        navigate(hostPath(recorded));
      }
    });
  };
  return (
    <SidePanel
      title={t("host.add.title")}
      subtitle={t("host.add.lead")}
      wide
      onSubmit={submit}
      onClose={onClose}
      footer={
        <>
          <Button variant="tertiary" onPress={onClose}>
            {t("shell.cancel")}
          </Button>
          <Button type="submit" isPending={add.pending}>
            {t("host.add.submit")}
          </Button>
        </>
      }
    >
      <HostFields
        values={values}
        onChange={setValues}
        spaces={spaces ?? [{ id: "", name: personal, personal: true }]}
        relays={(hosts ?? []).filter((h) => h.state === "managed" && h.relay === null)}
        attrs={attrs}
        clock={clock}
      />
      <Refusal error={add.error} />
    </SidePanel>
  );
}

/** blank are an empty form's values: the Host Account root and port 22 given, as most Hosts take them. */
const blank: HostValues = {
  name: "",
  space: "",
  address: "",
  account: "root",
  port: "22",
  relay: "",
  provider: "",
  region: "",
  price: "",
  currency: "",
  billing_cycle: "",
  expires_at: "",
  auto_renew: false,
  provider_account: "",
  rows: [],
  note: "",
};

/** newHost is the Host host.add records from the form: only the fields given, port left out when it is 22. */
export function newHost(values: HostValues, attrs: readonly AttrKey[], clock: Clock): NewHost {
  const host: NewHost = {
    name: values.name,
    space: values.space,
    address: values.address.trim(),
    account: values.account.trim(),
  };
  if (values.port !== "22") {
    host.port = Number(values.port);
  }
  if (values.relay !== "") {
    host.relay = values.relay;
  }
  const attributes = attributesOf(values, attrs, clock);
  if (Object.keys(attributes).length > 0) {
    host.attributes = attributes;
  }
  if (values.note.trim() !== "") {
    host.note = values.note;
  }
  return host;
}

