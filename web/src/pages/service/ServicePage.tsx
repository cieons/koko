import "../host/host.css";

import { useState } from "react";

import {
  type Service,
  ServiceRemove,
  ServiceRename,
  ServiceSet,
  type ServiceSetRequest,
  ServiceShow,
} from "../../api/service";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, useT } from "../../shell/i18n";
import { Link, navigate, type PageProps } from "../../shell/router";
import { hostPathOf, servicePath, splitPorts } from "../host/record";
import { ActionForm, Attributes, type Changes, EditRecord, Note } from "../host/RecordForm";

/** qualified names a Service as every command does: <host>/<service>. */
function qualified(service: Service): string {
  return `${service.host}/${service.name}`;
}

/**
 * onRetiredHost tells whether a Service's Host is Retired: the Service then
 * names it by its id, which holds a "_" no Host name holds.
 */
function onRetiredHost(service: Service): boolean {
  return service.host.includes("_");
}

/**
 * ServicePage shows a Service, as `koko service show` does: its fields,
 * Attributes and Note. It sets and clears its ports, Attributes and Note
 * and renames it, which never ask, and deletes it, which asks first. A
 * Service on a Retired Host is read-only.
 */
export function ServicePage({ params }: PageProps) {
  const t = useT();
  const show = useRead(ServiceShow, { host: params["host"] ?? "", name: params["name"] ?? "" });
  const [action, setAction] = useState<"edit" | "rename">();
  const [asking, setAsking] = useState(false);
  const remove = useWrite(ServiceRemove);
  if (!show.data) {
    return show.error !== undefined ? <ErrorMessage error={show.error} /> : null;
  }
  const { service } = show.data;
  const fields: [string, string][] = [
    ["id", service.id],
    ["ports", service.ports.join(", ")],
    ["created_at", formatDate(service.created_at)],
    ["updated_at", formatDate(service.updated_at)],
  ];

  return (
    <>
      <h1>{qualified(service)}</h1>
      {show.error !== undefined && <ErrorMessage error={show.error} />}
      <dl className="fields" role="group" aria-label={t("host.detail.fields")}>
        <div>
          <dt>host</dt>
          <dd>
            <Link to={hostPathOf(service.host)}>{service.host}</Link>
          </dd>
        </div>
        <div>
          <dt>name</dt>
          <dd>{service.name}</dd>
        </div>
        {fields.map(([key, value]) => (
          <div key={key}>
            <dt>{key}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      <Attributes attributes={service.attributes} />
      <Note note={service.note} />
      {onRetiredHost(service) ? (
        <p>{t("service.detail.readOnly")}</p>
      ) : (
        <div className="record-actions">
          <button type="button" onClick={() => setAction("edit")}>
            {t("service.edit.button")}
          </button>
          <button type="button" onClick={() => setAction("rename")}>
            {t("service.rename.button")}
          </button>
          <button type="button" onClick={() => setAsking(true)}>
            {t("service.remove.button")}
          </button>
        </div>
      )}
      {remove.error !== undefined && <ErrorMessage error={remove.error} />}
      {action === "edit" && (
        <EditService
          key={service.id}
          service={service}
          onDone={() => {
            setAction(undefined);
            show.reload();
          }}
          onCancel={() => setAction(undefined)}
        />
      )}
      {action === "rename" && <RenameService service={service} onCancel={() => setAction(undefined)} />}
      {asking && (
        <ConfirmDialog
          title={t("service.remove.title", { name: qualified(service) })}
          consequence={t("service.remove.consequence", { name: qualified(service) })}
          confirm={t("service.remove.confirm")}
          onCancel={() => setAsking(false)}
          onConfirm={() => {
            setAsking(false);
            void remove.submit({ host: service.host, name: service.name }).then((removed) => {
              if (removed) {
                navigate(hostPathOf(service.host), { replace: true });
              }
            });
          }}
        />
      )}
    </>
  );
}

/** EditService sets and clears a Service's ports, Attributes and Note, sending only those changed. */
function EditService({ service, onDone, onCancel }: { service: Service; onDone: () => void; onCancel: () => void }) {
  const t = useT();
  const set = useWrite(ServiceSet);
  const save = ({ fields, attributes, note, unset }: Changes) => {
    // Ports that split to none clear them, as service.set refuses "ports": [].
    const ports = fields["ports"] === undefined ? undefined : splitPorts(fields["ports"]);
    if (ports?.length === 0) {
      unset.push("ports");
    }
    const request: ServiceSetRequest = {
      host: service.host,
      name: service.name,
      ...(ports !== undefined && ports.length > 0 && { ports }),
      ...(Object.keys(attributes).length > 0 && { attributes }),
      ...(note !== undefined && { note }),
      ...(unset.length > 0 && { unset }),
    };
    void set.submit(request).then((written) => written && onDone());
  };
  return (
    <EditRecord
      title={t("service.edit.title", { name: qualified(service) })}
      own={[{ key: "ports", value: service.ports.join(", "), clearable: true }]}
      attributes={service.attributes}
      note={service.note}
      error={set.error}
      pending={set.pending}
      onSave={save}
      onCancel={onCancel}
    />
  );
}

/** RenameService gives a Service a new name on its Host, and the page moves to it. */
function RenameService({ service, onCancel }: { service: Service; onCancel: () => void }) {
  const t = useT();
  const [newName, setNewName] = useState("");
  const rename = useWrite(ServiceRename);
  return (
    <ActionForm
      title={t("service.rename.title", { name: qualified(service) })}
      submit={t("service.rename.submit")}
      ready={newName !== "" && !rename.pending}
      error={rename.error}
      onSubmit={() =>
        void rename.submit({ host: service.host, name: service.name, new_name: newName }).then((renamed) => {
          // The old name names no Service now, so it leaves no history entry.
          if (renamed) {
            navigate(servicePath(renamed.service.host, renamed.service.name), { replace: true });
          }
        })
      }
      onCancel={onCancel}
    >
      <label>
        {t("service.rename.name")}{" "}
        <input value={newName} onChange={(e) => setNewName(e.target.value)} autoComplete="off" spellCheck={false} />
      </label>
    </ActionForm>
  );
}
