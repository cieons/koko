import { Button, Switch } from "@heroui/react";
import { type ReactNode, useRef, useState } from "react";

import { type AttrKey, AttrList } from "../../api/attr";
import { type Host, HostAdd, HostList, HostSet, type HostSetRequest, type NewHost } from "../../api/host";
import type { Space } from "../../api/space";
import { useCaller } from "../../shell/caller";
import { reloadReads, useRead, useWrite } from "../../shell/data";
import { differs } from "../../shell/form";
import { useT } from "../../shell/i18n";
import { navigate } from "../../shell/router";
import { Field, type Option, SearchChoice } from "../../ui/fields";
import { notify, notifyRefusal } from "../../ui/notify";
import { SidePanel } from "../../ui/SidePanel";
import { Loaded, LoadFailed, Refusal } from "../../ui/states";
import { type AttributeRow, AttributeRows, recordChanges, type RecordText, rowsOf, text, typeOf } from "./AttributeRows";
import { DateValue } from "./DateValue";
import { EnrollFields, enrollRefusal, enrollValues, useEnroll } from "./Enroll";
import { EnumChoice } from "./EnumChoice";
import { purchase } from "./keys";
import { hostPath, validName, validPort } from "./record";
import { browserClock } from "./reldate";
import { type Clock, readValue } from "./view";

// The Host form (docs/web-design.md, "Add Host and Edit"): four groups, in
// the order of a provider's order confirmation. Add Host sends them in one
// host.add; Edit shows the same fields but Name and Space, and sends what
// changed in one host.set.

/** noRelay is the Relay choice's id for none: no Host's name, which never starts with '-'. */
const noRelay = "-";

/** unseenRelay is the Relay choice's id for a Relay the caller cannot see, which has no name to show. */
const unseenRelay = "-unseen";

/** maxRelays is the most Relays koko passes through to reach a Host (ADR 0055). */
const maxRelays = 4;

/**
 * relayChoices are the Hosts of hosts that may be the Relay of the Host
 * whose id is self, or of a new Host when self is not given: every Managed
 * Host but the Host itself and those its links show to be behind it, whose
 * chain, followed through each Host's own Relay as far as hosts holds it,
 * reaches it. A loop or a length through a Relay the caller cannot see is
 * the koko server's to refuse, and the form shows that refusal in place.
 */
export function relayChoices(hosts: readonly Host[], self?: string): Host[] {
  const relayOf = new Map(hosts.map((h) => [h.id, h.relay?.id ?? null]));
  const behind = (id: string): boolean => {
    let next = relayOf.get(id) ?? null;
    for (let hop = 0; next !== null && hop < maxRelays; hop++) {
      if (next === self) {
        return true;
      }
      next = relayOf.get(next) ?? null;
    }
    return false;
  };
  return hosts.filter((h) => h.state === "managed" && (self === undefined || (h.id !== self && !behind(h.id))));
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
  /** relays are the Hosts that may be the Relay, as relayChoices gives them. */
  relays: readonly Host[];
  /** relayNow, in Edit, is the Host's Relay as it stands, offered though relays may not hold it. */
  relayNow?: Option;
  /**
   * editing makes it Edit's form: the port may be emptied, back to 22; and
   * for a Retired Host, how koko reaches it shows but takes no change.
   */
  editing?: "host" | "retired";
  attrs: readonly AttrKey[];
  clock: Clock;
}

/**
 * HostFields are the Host form's four groups: the Host, how koko reaches
 * it, the purchase, and anything else. Each field checks its value when the
 * form is submitted, against its key's type as attr.list gives it.
 */
export function HostFields({ values, onChange, spaces, relays, relayNow, editing, attrs, clock }: HostFieldsProps) {
  const t = useT();
  const fixed = editing === "retired";
  const relayOptions = [
    { id: noRelay, label: t("host.form.noRelay") },
    ...(relayNow !== undefined && !relays.some((r) => r.name === relayNow.id) ? [relayNow] : []),
    ...relays.map((r) => ({ id: r.name, label: r.name })),
  ];
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
            validate={(v) => (v.trim() === "" ? t("host.form.required") : undefined)}
            isDisabled={fixed}
            code
            className="sm:col-span-2"
          />
        </div>
      </Group>
      <Group title={t("host.form.reach")}>
        <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
          <Field
            label={t("host.key.account")}
            value={values.account}
            onChange={set("account")}
            validate={(v) => (v.trim() === "" ? t("host.form.required") : undefined)}
            isDisabled={fixed}
            code
          />
          <Field
            label={t("host.key.port")}
            value={values.port}
            onChange={set("port")}
            description={editing === undefined ? undefined : t("host.form.portEmpty")}
            validate={(v) =>
              validPort(v.trim()) || (editing !== undefined && v.trim() === "") ? undefined : t("host.form.portRule")
            }
            isDisabled={fixed}
            inputMode="numeric"
            code
          />
          {relayOptions.length > 1 && (
            <SearchChoice
              label={t("host.key.relay")}
              value={values.relay === "" ? noRelay : values.relay}
              onChange={(v) => set("relay")(v === null || v === noRelay ? "" : v)}
              options={relayOptions}
              isDisabled={fixed}
              className="sm:col-span-2"
            />
          )}
        </div>
      </Group>
      <Group title={t("host.form.purchase")}>
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
            none={t("host.form.noValue")}
            className="sm:col-span-2"
          />
          <EnumChoice
            label={t("host.key.billing_cycle")}
            type={typeOf(attrs, "billing_cycle")}
            value={values.billing_cycle}
            onChange={set("billing_cycle")}
            none={t("host.form.noValue")}
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
            validate={ofItsType("provider_account")}
            className="sm:col-span-6"
          />
        </div>
      </Group>
      <Group title={t("host.form.more")} lead={t("host.form.moreLead")}>
        <AttributeRows rows={values.rows} onChange={set("rows")} reserved={purchase} attrs={attrs} clock={clock} />
        <Field
          label={t("host.key.note")}
          value={values.note}
          onChange={set("note")}
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
 * another; then, when a way to Enroll is given, one host.enroll naming the
 * new Host by its id, which clears the credential as it is sent; then the
 * page moves to the new Host. When the Enroll fails, the Host stays
 * recorded, Registered, and the panel says so with the Enroll's refusal,
 * and offers the Host's page, where Enroll is offered again.
 *
 * Closed while a call runs, it sends nothing more, the credential with
 * it, and moves nowhere: an answer that comes after says in a toast what
 * was done, and the list under it loads again.
 */
export function AddHost({ spaces, hosts, attrs = [], onClose }: AddHostProps) {
  const t = useT();
  const caller = useCaller();
  const add = useWrite(HostAdd);
  const personal = spaces?.find((s) => s.personal && s.name === caller.user.name)?.name ?? caller.user.name;
  const [opened] = useState<HostValues>(() => ({
    ...blank,
    space: personal,
  }));
  const [values, setValues] = useState(opened);
  const [enrollOpened] = useState(() => enrollValues("none"));
  const [enrolling, setEnrolling] = useState(enrollOpened);
  const enroll = useEnroll(setEnrolling);
  // notEnrolled is the Host recorded whose Enroll failed.
  const [notEnrolled, setNotEnrolled] = useState<Host>();
  // closed tells a call's answer that the panel has closed; recorded, that host.add has answered.
  const closed = useRef(false);
  const recorded = useRef(false);
  const clock = browserClock();
  const open = (h: Host) => {
    onClose();
    navigate(hostPath(h));
  };
  const close = () => {
    closed.current = true;
    // The list under the panel shows the Host recorded.
    if (recorded.current) {
      reloadReads();
    }
    onClose();
  };
  const submit = () => {
    if (notEnrolled !== undefined) {
      open(notEnrolled);
      return;
    }
    void add.submit({ hosts: [newHost(values, attrs, clock)] }).then(async (added) => {
      const [h] = added?.hosts ?? [];
      if (h === undefined) {
        return;
      }
      recorded.current = true;
      if (closed.current || enrolling.way === "none") {
        notify(t("host.add.done", { name: h.name }));
        if (closed.current) {
          reloadReads();
        } else {
          open(h);
        }
        return;
      }
      const { response, error } = await enroll.send(h.id, enrolling);
      if (closed.current) {
        if (response === undefined) {
          notifyRefusal(t("host.add.notEnrolled", { name: h.name, reason: enrollRefusal(t, error) ?? "" }));
        } else {
          notify(t("host.add.enrolled", { name: h.name, fingerprint: response.host.host_key ?? "" }));
        }
        reloadReads();
      } else if (response === undefined) {
        setNotEnrolled(h);
      } else {
        notify(t("host.add.enrolled", { name: h.name, fingerprint: response.host.host_key ?? "" }));
        open(response.host);
      }
    });
  };
  const pending = add.pending || enroll.pending;
  return (
    <SidePanel
      title={t("host.add.title")}
      wide
      onSubmit={submit}
      changed={notEnrolled === undefined && (differs(values, opened) || differs(enrolling, enrollOpened))}
      pending={pending}
      onClose={close}
      footer={
        <>
          <Button variant="tertiary" onPress={close}>
            {notEnrolled === undefined ? t("shell.cancel") : t("shell.close")}
          </Button>
          <Button type="submit" isPending={pending}>
            {notEnrolled === undefined ? t("host.add.submit") : t("host.add.openHost", { name: notEnrolled.name })}
          </Button>
        </>
      }
    >
      {notEnrolled === undefined ? (
        <>
          <HostFields
            values={values}
            onChange={setValues}
            spaces={spaces ?? [{ id: "", name: personal, personal: true }]}
            relays={relayChoices(hosts ?? [])}
            attrs={attrs}
            clock={clock}
          />
          <Group title={t("host.enroll")}>
            <EnrollFields values={enrolling} onChange={setEnrolling} account={values.account.trim()} optional />
          </Group>
          <Refusal error={add.error} />
        </>
      ) : (
        <Refusal
          error={t("host.add.notEnrolled", { name: notEnrolled.name, reason: enrollRefusal(t, enroll.error) ?? "" })}
        />
      )}
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

/**
 * formValuesOf are the Host form's values for host as it stands, which Edit
 * opens with and sends only the changes from: its Relay by its name, or as
 * one the caller cannot see.
 */
export function formValuesOf(host: Host): HostValues {
  const a = host.attributes;
  return {
    name: host.name,
    space: host.space,
    address: host.address,
    account: host.account,
    port: String(host.port),
    relay: host.relay === null ? "" : (host.relay.name ?? unseenRelay),
    provider: text(a.provider),
    region: text(a.region),
    price: text(a.price),
    currency: text(a.currency),
    billing_cycle: text(a.billing_cycle),
    expires_at: text(a.expires_at),
    auto_renew: a.auto_renew === "yes",
    provider_account: text(a.provider_account),
    rows: rowsOf(a, purchase),
    note: host.note ?? "",
  };
}

/**
 * hostChanges are what Edit sends in host.set: only the fields that differ
 * from before, the record as the form opened with it, so a field someone
 * else changed meanwhile is never overwritten; and unset for the port, the
 * Relay, an Attribute and the Note emptied. The port emptied goes back to
 * 22; Renews automatically switched off is no.
 */
export function hostChanges(
  before: HostValues,
  after: HostValues,
  attrs: readonly AttrKey[],
  clock: Clock,
): Omit<HostSetRequest, "host"> {
  const changes: Omit<HostSetRequest, "host"> = {};
  const unset: string[] = [];
  if (after.address.trim() !== before.address) {
    changes.address = after.address.trim();
  }
  if (after.account.trim() !== before.account) {
    changes.account = after.account.trim();
  }
  const port = after.port.trim();
  if (port !== before.port) {
    if (port === "") {
      unset.push("port");
    } else {
      changes.port = Number(port);
    }
  }
  if (after.relay !== before.relay) {
    if (after.relay === "") {
      unset.push("relay");
    } else {
      changes.relay = after.relay;
    }
  }
  const record = recordChanges(recordText(before), recordText(after), attrs, clock);
  const attributes = { ...record.attributes };
  // Renews automatically is a switch: on is yes, and off, once it was on, is no.
  if (after.auto_renew !== before.auto_renew) {
    attributes.auto_renew = after.auto_renew ? "yes" : "no";
  }
  if (Object.keys(attributes).length > 0) {
    changes.attributes = attributes;
  }
  if (record.note !== undefined) {
    changes.note = record.note;
  }
  unset.push(...record.unset);
  if (unset.length > 0) {
    changes.unset = unset;
  }
  return changes;
}

/** recordText is what the Host form holds of a Host's Attributes and Note as typed, Renews automatically aside. */
function recordText(values: HostValues): RecordText {
  return {
    attributes: new Map([
      ...purchase.filter((k) => k !== "auto_renew").map((k): [string, string] => [k, values[k]]),
      ...values.rows.map((r): [string, string] => [r.key, r.value]),
    ]),
    note: values.note,
  };
}

/** EditHostProps are what Edit is drawn from. */
export interface EditHostProps {
  host: Host;
  /** onSaved hears that host.set wrote the changes. */
  onSaved: () => void;
  onClose: () => void;
}

/**
 * EditHost is Edit (docs/web-design.md, "Add Host and Edit"): the Host form
 * but Name and Space, which Rename and Move change, in a wide side panel. It
 * sends one host.set, naming the Host by its id, with only what changed and
 * unset for what was emptied; with nothing changed it closes, sending
 * nothing. A Retired Host's form takes its Attributes and Note alone, and
 * says so.
 */
export function EditHost({ host, onSaved, onClose }: EditHostProps) {
  const t = useT();
  const attrs = useRead(AttrList, {});
  const hosts = useRead(HostList, {});
  const set = useWrite(HostSet);
  const [before] = useState(() => formValuesOf(host));
  const [values, setValues] = useState(before);
  const clock = browserClock();
  const retired = host.state === "retired";
  const relayNow =
    host.relay === null
      ? undefined
      : { id: before.relay, label: host.relay.name ?? t("host.relay.unseen") };
  const submit = () => {
    const changes = hostChanges(before, values, attrs.data?.attributes ?? [], clock);
    if (Object.keys(changes).length === 0) {
      onClose();
      return;
    }
    void set.submit({ host: host.id, ...changes }).then((written) => {
      if (written) {
        notify(t("host.edit.done", { name: host.name }));
        onSaved();
      }
    });
  };
  return (
    <SidePanel
      title={t("host.edit.title", { name: host.name })}
      subtitle={retired ? t("host.edit.retired") : undefined}
      wide
      onSubmit={submit}
      changed={differs(values, before)}
      pending={set.pending}
      onClose={onClose}
      footer={
        <>
          <Button variant="tertiary" onPress={onClose}>
            {t("shell.cancel")}
          </Button>
          <Button type="submit" isPending={set.pending}>
            {t("host.edit.submit")}
          </Button>
        </>
      }
    >
      <Loaded read={attrs}>
        {({ attributes }) => (
          <HostFields
            values={values}
            onChange={setValues}
            relays={relayChoices(hosts.data?.hosts ?? [], host.id)}
            relayNow={relayNow}
            editing={retired ? "retired" : "host"}
            attrs={attributes}
            clock={clock}
          />
        )}
      </Loaded>
      {hosts.error !== undefined && <LoadFailed error={hosts.error} retry={hosts.reload} />}
      <Refusal error={set.error} />
    </SidePanel>
  );
}
