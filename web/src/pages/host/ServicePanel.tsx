import { Button } from "@heroui/react";
import { useState } from "react";

import { type AttrKey, AttrList } from "../../api/attr";
import type { Host } from "../../api/host";
import {
  type Service,
  ServiceAdd,
  type ServiceAddRequest,
  ServiceRemove,
  ServiceRename,
  ServiceSet,
  type ServiceSetRequest,
  ServiceShow,
} from "../../api/service";
import { useRead, useWrite } from "../../shell/data";
import { differs } from "../../shell/form";
import { useT } from "../../shell/i18n";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { Field } from "../../ui/fields";
import { notify } from "../../ui/notify";
import { SidePanel } from "../../ui/SidePanel";
import { Loaded, LoadFailed, Refusal } from "../../ui/states";
import { type AttributeRow, AttributeRows, recordChanges, type RecordText, rowsOf } from "./AttributeRows";
import { Fact } from "./Fact";
import { keyLabel } from "./keys";
import { splitPorts, validName, validServicePort } from "./record";
import { browserClock } from "./reldate";
import type { Clock } from "./view";

// A Service's panel over its Host's page (docs/web-design.md, "A Service"):
// its name, ports, Attributes and Note, saved in one service.set and then
// service.rename; the same panel, empty, adds one.

/** ServiceValues are a Service's panel's values, as typed. */
interface ServiceValues {
  name: string;
  /** ports are the ports as written, separated by commas or spaces. */
  ports: string;
  rows: AttributeRow[];
  note: string;
}

/** blank are an empty panel's values, for Add Service. */
const blank: ServiceValues = { name: "", ports: "", rows: [], note: "" };

/** valuesOf are the panel's values for service as it stands. */
function valuesOf(service: Service): ServiceValues {
  return {
    name: service.name,
    ports: service.ports.join(", "),
    rows: rowsOf(service.attributes),
    note: service.note ?? "",
  };
}

/** recordText is what the panel holds of a Service's Attributes and Note, as typed. */
function recordText(values: ServiceValues): RecordText {
  return { attributes: new Map(values.rows.map((r): [string, string] => [r.key, r.value])), note: values.note };
}

/**
 * serviceChanges are what Save sends in service.set: the ports, Attributes
 * and Note that differ from before, as the panel opened with them, and
 * unset for those emptied. The name goes in service.rename.
 */
function serviceChanges(
  before: ServiceValues,
  after: ServiceValues,
  attrs: readonly AttrKey[],
  clock: Clock,
): Omit<ServiceSetRequest, "host" | "name"> {
  const { unset, ...changes }: Omit<ServiceSetRequest, "host" | "name"> & { unset: string[] } = recordChanges(
    recordText(before),
    recordText(after),
    attrs,
    clock,
  );
  const ports = splitPorts(after.ports);
  if (ports.join(", ") !== splitPorts(before.ports).join(", ")) {
    if (ports.length === 0) {
      unset.unshift("ports");
    } else {
      changes.ports = ports;
    }
  }
  return unset.length > 0 ? { ...changes, unset } : changes;
}

/** newService is the Service service.add records from the panel: only what is given. */
function newService(host: Host, values: ServiceValues, attrs: readonly AttrKey[], clock: Clock): ServiceAddRequest {
  const service: ServiceAddRequest = { host: host.id, name: values.name.trim() };
  const ports = splitPorts(values.ports);
  if (ports.length > 0) {
    service.ports = ports;
  }
  const given = recordChanges(recordText(blank), recordText(values), attrs, clock);
  return { ...service, ...(given.attributes && { attributes: given.attributes }), ...(given.note && { note: given.note }) };
}

/** ServiceFields are the panel's fields: Name, Ports, the Attribute rows and the Note. */
function ServiceFields({
  values,
  onChange,
  adding = false,
  attrs,
  clock,
}: {
  values: ServiceValues;
  onChange: (values: ServiceValues) => void;
  /** adding is Add Service's panel, whose Name takes the focus as it opens. */
  adding?: boolean;
  attrs: readonly AttrKey[];
  clock: Clock;
}) {
  const t = useT();
  const set = <K extends keyof ServiceValues>(key: K) => (value: ServiceValues[K]) => onChange({ ...values, [key]: value });
  return (
    <>
      <Field
        label={t("host.key.name")}
        value={values.name}
        onChange={set("name")}
        validate={(v) => (validName(v.trim()) ? undefined : t("host.form.nameRule"))}
        code
        autoFocus={adding}
      />
      <Field
        label={t("host.key.ports")}
        value={values.ports}
        onChange={set("ports")}
        description={t("service.portsHint")}
        validate={(v) => (splitPorts(v).every(validServicePort) ? undefined : t("service.portsRule"))}
        placeholder="443/tcp, 51820/udp"
        code
      />
      <AttributeRows rows={values.rows} onChange={set("rows")} attrs={attrs} clock={clock} />
      <Field label={t("host.key.note")} value={values.note} onChange={set("note")} multiline rows={4} />
    </>
  );
}

/** PanelProps are what a Service's panel is opened over. */
interface PanelProps {
  host: Host;
  onClose: () => void;
}

/**
 * AddService is Add Service: the panel, empty, which sends one
 * service.add, naming the Host by its id, then opens the new Service's
 * panel through onAdded.
 */
export function AddService({ host, onClose, onAdded }: PanelProps & { onAdded: (service: Service) => void }) {
  const t = useT();
  const attrs = useRead(AttrList, {});
  const add = useWrite(ServiceAdd);
  const [values, setValues] = useState(blank);
  const submit = () => {
    void add.submit(newService(host, values, attrs.data?.attributes ?? [], browserClock())).then((added) => {
      if (added) {
        notify(t("service.added", { name: added.service.name }));
        onAdded(added.service);
      }
    });
  };
  return (
    <SidePanel
      title={t("service.add")}
      subtitle={t("service.on", { host: host.name })}
      onSubmit={submit}
      changed={differs(values, blank)}
      pending={add.pending}
      onClose={onClose}
      footer={
        <>
          <Button variant="tertiary" onPress={onClose}>
            {t("shell.cancel")}
          </Button>
          <Button type="submit" isPending={add.pending}>
            {t("service.add")}
          </Button>
        </>
      }
    >
      {/* The fields show at once, so Name takes the focus as the panel opens; attr.list's types reach a row as it answers. */}
      <ServiceFields
        values={values}
        onChange={setValues}
        adding
        attrs={attrs.data?.attributes ?? []}
        clock={browserClock()}
      />
      {attrs.error !== undefined && <LoadFailed error={attrs.error} retry={attrs.reload} />}
      <Refusal error={add.error} />
    </SidePanel>
  );
}

/** OpenServiceProps are what a Service's panel shows, and whom it tells of a change. */
interface OpenServiceProps extends PanelProps {
  /** name is the Service's name on host, as the URL gives it. */
  name: string;
  /** onChanged hears that a write changed or deleted the Service, so the Host's page loads again. */
  onChanged: () => void;
}

/**
 * OpenService is a Service's panel, open at /hosts/<host>/services/<name>:
 * what service.show answers, naming the Host by its id. On a Host that is
 * not Retired, Save sends one service.set with what changed, then
 * service.rename when the name changed, and says what was saved when the
 * rename is refused; Delete asks first. On a Retired Host the panel shows
 * the Service and offers no action. It is one panel throughout, so the
 * focus it takes and gives back is not lost as its read answers.
 */
export function OpenService({ host, name, onClose, onChanged }: OpenServiceProps) {
  const t = useT();
  const show = useRead(ServiceShow, { host: host.id, name });
  const attrs = useRead(AttrList, {});
  const set = useWrite(ServiceSet);
  const rename = useWrite(ServiceRename);
  const [before, setBefore] = useState<ServiceValues>();
  const [values, setValues] = useState<ServiceValues>();
  const [savedApart, setSavedApart] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const service = show.data?.service;
  // The panel's values start from the Service as service.show first answers it.
  if (service !== undefined && before === undefined) {
    const shown = valuesOf(service);
    setBefore(shown);
    setValues(shown);
  }
  const editing = host.state === "retired" || service === undefined ? undefined : { service, before, values };
  const submit = async (shown: Service, from: ServiceValues, typed: ServiceValues) => {
    const changes = serviceChanges(from, typed, attrs.data?.attributes ?? [], browserClock());
    let saved = savedApart;
    if (Object.keys(changes).length > 0) {
      if (!(await set.submit({ host: host.id, name: shown.name, ...changes }))) {
        return;
      }
      saved = true;
      setBefore({ ...typed, name: from.name });
    }
    const newName = typed.name.trim();
    const renaming = newName !== shown.name;
    if (renaming && !(await rename.submit({ host: host.id, name: shown.name, new_name: newName }))) {
      setSavedApart(saved);
      if (saved) {
        onChanged();
      }
      return;
    }
    if (!saved && !renaming) {
      onClose(); // nothing changed, so nothing was sent
      return;
    }
    notify(t("service.saved", { name: newName }));
    onChanged();
    onClose();
  };
  if (editing?.before === undefined || editing.values === undefined) {
    return (
      <SidePanel title={service?.name ?? name} subtitle={t("service.on", { host: host.name })} onClose={onClose}>
        <Loaded read={show}>{({ service: shown }) => <ServiceFacts service={shown} />}</Loaded>
      </SidePanel>
    );
  }
  const { before: from, values: typed } = editing;
  return (
    <SidePanel
      title={editing.service.name}
      subtitle={t("service.on", { host: host.name })}
      onSubmit={() => void submit(editing.service, from, typed)}
      changed={differs(typed, from)}
      pending={set.pending || rename.pending}
      onClose={onClose}
      footer={
        <>
          <Button variant="danger-soft" className="mr-auto" onPress={() => setDeleting(true)}>
            {t("service.remove")}
          </Button>
          <Button variant="tertiary" onPress={onClose}>
            {t("shell.cancel")}
          </Button>
          <Button type="submit" isPending={set.pending || rename.pending}>
            {t("service.save")}
          </Button>
        </>
      }
    >
      <Loaded read={attrs}>
        {({ attributes }) => (
          <ServiceFields values={typed} onChange={setValues} attrs={attributes} clock={browserClock()} />
        )}
      </Loaded>
      {savedApart && (
        <p role="status" className="t-label">
          {t("service.savedNotRenamed", { name: editing.service.name })}
        </p>
      )}
      <Refusal error={set.error ?? rename.error} />
      {deleting && (
        <DeleteService
          host={host}
          service={editing.service}
          onCancel={() => setDeleting(false)}
          onDeleted={() => {
            onChanged();
            onClose();
          }}
        />
      )}
    </SidePanel>
  );
}

/** DeleteService asks first, then deletes the Service's record, naming its Host by its id. */
function DeleteService({
  host,
  service,
  onCancel,
  onDeleted,
}: {
  host: Host;
  service: Service;
  onCancel: () => void;
  onDeleted: () => void;
}) {
  const t = useT();
  const remove = useWrite(ServiceRemove);
  return (
    <ConfirmDialog
      title={t("service.remove.title", { name: service.name })}
      consequence={t("service.remove.consequence", { name: service.name, host: host.name })}
      confirm={t("service.remove")}
      pending={remove.pending}
      error={remove.error}
      onConfirm={() =>
        void remove.submit({ host: host.id, name: service.name }).then((removed) => {
          if (removed) {
            notify(t("service.remove.done", { name: service.name }));
            onDeleted();
          }
        })
      }
      onCancel={onCancel}
    />
  );
}

/** ServiceFacts are a Service on a Retired Host, as it was kept: its ports, Attributes and Note, read-only. */
function ServiceFacts({ service }: { service: Service }) {
  const t = useT();
  const attributes = rowsOf(service.attributes);
  return (
    <>
      <p className="text-muted">{t("service.readOnly")}</p>
      <dl className="flex flex-col gap-5">
        <Fact label={t("host.key.ports")}>
          {service.ports.length === 0 ? (
            <span className="text-muted">{t("host.services.noPorts")}</span>
          ) : (
            <span className="t-code">{service.ports.join(", ")}</span>
          )}
        </Fact>
        <Fact label={t("host.page.attributes")}>
          {attributes.length === 0 ? (
            <span className="text-muted">{t("service.noAttributes")}</span>
          ) : (
            <dl className="grid grid-cols-2 gap-x-6 gap-y-3">
              {attributes.map((a) => (
                <Fact key={a.key} label={keyLabel(t, a.key)}>
                  {a.value}
                </Fact>
              ))}
            </dl>
          )}
        </Fact>
        <Fact label={t("host.key.note")}>
          {service.note === undefined ? (
            <span className="text-muted">{t("host.page.noNote")}</span>
          ) : (
            <span className="whitespace-pre-wrap">{service.note}</span>
          )}
        </Fact>
      </dl>
    </>
  );
}
