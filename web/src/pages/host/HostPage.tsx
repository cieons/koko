import "./host.css";

import { type ReactNode, useState } from "react";

import {
  type Host,
  HostMove,
  HostRemove,
  HostRename,
  HostRetire,
  HostSet,
  type HostSetRequest,
  HostShow,
} from "../../api/host";
import { type Service, ServiceAdd } from "../../api/service";
import { SpaceList } from "../../api/space";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, useT } from "../../shell/i18n";
import { Link, navigate, type PageProps } from "../../shell/router";
import { hostPath, hostPathOf, servicePath, splitPorts, validPort } from "./record";
import { ActionForm, Attributes, type Changes, EditRecord, Field, Note } from "./RecordForm";

/** Action is the reversible action whose form is open, if any. */
type Action = "edit" | "rename" | "move";

/**
 * HostPage shows a Host the caller can see, named by its name or, once it
 * is Retired, its id, as `koko host show` does: its fields, Attributes, Note,
 * Services and pinned host key's fingerprint, and for a Registered Host the
 * `koko host enroll` line to copy, since Enroll stays in the CLI. It sets,
 * clears, renames and moves the Host, which never ask, and retires and
 * deletes it, which ask first and say what follows. A Retired Host takes
 * only changes to its Attributes and Note, and moves, and its Services are
 * read-only.
 */
export function HostPage({ params }: PageProps) {
  const t = useT();
  const named = params["host"] ?? "";
  const show = useRead(HostShow, { host: named });
  const [action, setAction] = useState<Action>();
  const [asking, setAsking] = useState<"retire" | "remove">();
  const retire = useWrite(HostRetire);
  const remove = useWrite(HostRemove);
  if (!show.data) {
    return show.error !== undefined ? <ErrorMessage error={show.error} /> : null;
  }
  const { host, services } = show.data;
  const retired = host.state === "retired";
  const done = () => {
    setAction(undefined);
    show.reload();
  };

  return (
    <>
      <h1>
        {host.name}
        {retired && <span className="tag"> ({t("host.detail.retired")})</span>}
      </h1>
      {show.error !== undefined && <ErrorMessage error={show.error} />}
      <HostFields host={host} />
      {host.state === "registered" && <EnrollLine host={host.name} />}
      <Attributes attributes={host.attributes} />
      <Note note={host.note} />
      <Services host={host} services={services} />
      <div className="record-actions">
        <button type="button" onClick={() => setAction("edit")}>
          {t("host.edit.button")}
        </button>
        {!retired && (
          <button type="button" onClick={() => setAction("rename")}>
            {t("host.rename.button")}
          </button>
        )}
        <button type="button" onClick={() => setAction("move")}>
          {t("host.move.button")}
        </button>
        {!retired && (
          <button type="button" onClick={() => setAsking("retire")}>
            {t("host.retire.button")}
          </button>
        )}
        {host.host_key === null && (
          <button type="button" onClick={() => setAsking("remove")}>
            {t("host.remove.button")}
          </button>
        )}
      </div>
      {retire.error !== undefined && <ErrorMessage error={retire.error} />}
      {remove.error !== undefined && <ErrorMessage error={remove.error} />}
      {action === "edit" && (
        <EditHost key={host.id} host={host} onDone={done} onCancel={() => setAction(undefined)} />
      )}
      {action === "rename" && <RenameHost host={host} onCancel={() => setAction(undefined)} />}
      {action === "move" && <MoveHost host={host} onDone={done} onCancel={() => setAction(undefined)} />}
      {asking === "retire" && (
        <ConfirmDialog
          title={t("host.retire.title", { name: host.name })}
          consequence={t("host.retire.consequence", { name: host.name, id: host.id })}
          confirm={t("host.retire.confirm")}
          onCancel={() => setAsking(undefined)}
          onConfirm={() => {
            setAsking(undefined);
            void retire.submit({ host: host.id }).then((retired) => {
              // Its name no longer names it, so the page moves to its id.
              if (retired) {
                navigate(hostPath(retired.host), { replace: true });
              }
            });
          }}
        />
      )}
      {asking === "remove" && (
        <ConfirmDialog
          title={t("host.remove.title", { name: host.name })}
          consequence={
            services.length === 0
              ? t("host.remove.consequenceAlone", { name: host.name })
              : t("host.remove.consequence", { name: host.name, services: services.length })
          }
          confirm={t("host.remove.confirm")}
          onCancel={() => setAsking(undefined)}
          onConfirm={() => {
            setAsking(undefined);
            void remove.submit({ host: host.id }).then((removed) => {
              if (removed) {
                navigate("/hosts", { replace: true });
              }
            });
          }}
        />
      )}
    </>
  );
}

/** HostFields are a Host's fields by key, as `koko host show` prints them. */
function HostFields({ host }: { host: Host }) {
  const t = useT();
  const relay =
    host.relay === null ? (
      t("host.relay.none")
    ) : host.relay.id === null || host.relay.name === null ? (
      t("host.relay.unseen")
    ) : (
      <Link to={hostPathOf(host.relay.id)}>{host.relay.name}</Link>
    );
  const rows: [string, ReactNode][] = [
    ["id", host.id],
    ["space", host.space],
    ["state", host.state],
    ["address", host.address],
    ["port", host.port],
    ["account", host.account],
    ["relay", relay],
    ["host_key", host.host_key ?? t("host.hostKey.none")],
    ["created_at", formatDate(host.created_at)],
    ["updated_at", formatDate(host.updated_at)],
  ];
  return (
    <dl className="fields" role="group" aria-label={t("host.detail.fields")}>
      {rows.map(([key, value]) => (
        <div key={key}>
          <dt>{key}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** EnrollLine is the `koko host enroll <host>` line a Registered Host shows, with a button that copies it. */
function EnrollLine({ host }: { host: string }) {
  const t = useT();
  const [copied, setCopied] = useState<boolean>();
  const line = `koko host enroll ${host}`;
  // A browser without the clipboard API, or one that refuses it, still
  // shows the line to copy by hand.
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(line);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <p>
      {t("host.enroll.hint")} <code>{line}</code>{" "}
      <button type="button" onClick={() => void copy()}>
        {t("host.enroll.copy")}
      </button>{" "}
      {copied !== undefined && (
        <span role="status">{copied ? t("host.enroll.copied") : t("host.enroll.copyFailed")}</span>
      )}
    </p>
  );
}

/**
 * Services lists the Services on a Host, each linking to its page, and
 * adds one, which never asks; a Retired Host's Services are read-only.
 */
function Services({ host, services }: { host: Host; services: readonly Service[] }) {
  const t = useT();
  return (
    <>
      <h2>{t("host.services")}</h2>
      {services.length === 0 ? (
        <p>{t("host.services.none")}</p>
      ) : (
        <table aria-label={t("host.services")}>
          <thead>
            <tr>
              <th>name</th>
              <th>ports</th>
            </tr>
          </thead>
          <tbody>
            {services.map((s) => (
              <tr key={s.id}>
                <td>
                  <Link to={servicePath(s.host, s.name)}>{s.name}</Link>
                </td>
                <td>{s.ports.join(", ")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {host.state !== "retired" && <AddService host={host.id} />}
    </>
  );
}

/** AddService records a Service on the Host whose id is host, and the page moves to it. */
function AddService({ host }: { host: string }) {
  const t = useT();
  const [name, setName] = useState("");
  const [ports, setPorts] = useState("");
  const add = useWrite(ServiceAdd);
  return (
    <ActionForm
      title={t("host.service.add.title")}
      submit={t("host.service.add.submit")}
      ready={name !== "" && !add.pending}
      error={add.error}
      onSubmit={() => {
        const listed = splitPorts(ports);
        void add.submit({ host, name, ...(listed.length > 0 && { ports: listed }) }).then((added) => {
          if (added) {
            navigate(servicePath(added.service.host, added.service.name));
          }
        });
      }}
    >
      <Field name="name" value={name} onChange={setName} />
      <Field name="ports" value={ports} onChange={setPorts} />
    </ActionForm>
  );
}

/** ChangeProps are what the form of a reversible action on a Host gets. */
interface ChangeProps {
  host: Host;
  /** onDone hears that the write succeeded. */
  onDone: () => void;
  onCancel: () => void;
}

/**
 * EditHost sets and clears a Host's address, port, Host Account, Relay,
 * Attributes and Note, sending only those changed; a Retired Host's form
 * offers only its Attributes and Note.
 */
function EditHost({ host, onDone, onCancel }: ChangeProps) {
  const t = useT();
  const set = useWrite(HostSet);
  const own =
    host.state === "retired"
      ? []
      : [
          { key: "address", value: host.address, clearable: false },
          { key: "port", value: String(host.port), clearable: true, valid: validPort },
          { key: "account", value: host.account, clearable: false },
          // A Relay the caller cannot see shows as such, so emptying it clears it.
          { key: "relay", value: host.relay ? (host.relay.name ?? t("host.relay.unseen")) : "", clearable: true },
        ];
  const save = ({ fields, attributes, note, unset }: Changes) => {
    const { address, port, account, relay } = fields;
    const request: HostSetRequest = {
      host: host.id,
      ...(address !== undefined && { address }),
      ...(port !== undefined && { port: Number(port) }),
      ...(account !== undefined && { account }),
      ...(relay !== undefined && { relay }),
      ...(Object.keys(attributes).length > 0 && { attributes }),
      ...(note !== undefined && { note }),
      ...(unset.length > 0 && { unset }),
    };
    void set.submit(request).then((written) => written && onDone());
  };
  return (
    <EditRecord
      title={t("host.edit.title", { name: host.name })}
      own={own}
      attributes={host.attributes}
      note={host.note}
      error={set.error}
      pending={set.pending}
      onSave={save}
      onCancel={onCancel}
    />
  );
}

/** RenameHost gives a Host a new name, its SSH Name, and the page moves to it. */
function RenameHost({ host, onCancel }: Omit<ChangeProps, "onDone">) {
  const t = useT();
  const [newName, setNewName] = useState("");
  const rename = useWrite(HostRename);
  return (
    <ActionForm
      title={t("host.rename.title", { name: host.name })}
      submit={t("host.rename.submit")}
      ready={newName !== "" && !rename.pending}
      error={rename.error}
      onSubmit={() =>
        void rename.submit({ host: host.id, new_name: newName }).then((renamed) => {
          // The old name names no Host now, so it leaves no history entry.
          if (renamed) {
            navigate(hostPath(renamed.host), { replace: true });
          }
        })
      }
      onCancel={onCancel}
    >
      <label>
        {t("host.rename.name")}{" "}
        <input value={newName} onChange={(e) => setNewName(e.target.value)} autoComplete="off" spellCheck={false} />
      </label>
    </ActionForm>
  );
}

/** MoveHost moves a Host, under its name, to another Space the caller can act in. */
function MoveHost({ host, onDone, onCancel }: ChangeProps) {
  const t = useT();
  const spaces = useRead(SpaceList, {});
  const [space, setSpace] = useState("");
  const move = useWrite(HostMove);
  return (
    <ActionForm
      title={t("host.move.title", { name: host.name })}
      submit={t("host.move.submit")}
      ready={space !== "" && !move.pending}
      error={move.error ?? spaces.error}
      onSubmit={() => void move.submit({ host: host.id, space }).then((moved) => moved && onDone())}
      onCancel={onCancel}
    >
      <label>
        {t("host.move.space")}{" "}
        <select value={space} onChange={(e) => setSpace(e.target.value)}>
          <option value="">{t("host.move.choose")}</option>
          {(spaces.data?.spaces ?? [])
            .filter((s) => s.name !== host.space)
            .map((s) => (
              <option key={s.id} value={s.name}>
                {s.name}
              </option>
            ))}
        </select>
      </label>
    </ActionForm>
  );
}
