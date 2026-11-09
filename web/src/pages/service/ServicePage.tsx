import "../host/host.css";

import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { Layout, LayoutContent, LayoutPanel, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Spinner } from "@astryxdesign/core/Spinner";
import { useState } from "react";

import {
  type Service,
  ServiceRemove,
  ServiceRename,
  ServiceSet,
  type ServiceSetRequest,
  ServiceShow,
} from "../../api/service";
import { FormDialog } from "../../shell/AdaptiveDialog";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, useT } from "../../shell/i18n";
import { navigate, type PageProps } from "../../shell/router";
import { hostPathOf, qualified, servicePath, splitPorts } from "../host/record";
import { type Changes, EditRecord, Field } from "../host/RecordForm";
import { Attributes, FieldList, narrow, Note, RecordHeader } from "../host/RecordView";

/**
 * onRetiredHost tells whether a Service's Host is Retired: the Service then
 * names it by its id, which holds a "_" no Host name holds.
 */
function onRetiredHost(service: Service): boolean {
  return service.host.includes("_");
}

/**
 * ServicePage shows a Service, as `koko service show` does, on the Host
 * page's pattern: a summary header with its name and actions, its fields
 * and Attributes, and its Note in the rail. It sets and clears its ports,
 * Attributes and Note and renames it, which never ask, and deletes it,
 * which asks first. A Service on a Retired Host is read-only.
 */
export function ServicePage({ params }: PageProps) {
  const t = useT();
  const show = useRead(ServiceShow, { host: params["host"] ?? "", name: params["name"] ?? "" });
  const [action, setAction] = useState<"edit" | "rename" | "remove">();
  const remove = useWrite(ServiceRemove);
  const isNarrow = useMediaQuery(narrow);
  if (!show.data) {
    return show.error !== undefined ? <ErrorMessage error={show.error} /> : <Spinner label={t("shell.loading")} />;
  }
  const { service } = show.data;
  const readOnly = onRetiredHost(service);
  const close = () => setAction(undefined);
  const actions = readOnly
    ? []
    : [
        <Button key="edit" label={t("service.edit.button")} onClick={() => setAction("edit")} />,
        <Button key="rename" label={t("service.rename.button")} onClick={() => setAction("rename")} />,
        <Button key="remove" label={t("service.remove.button")} onClick={() => setAction("remove")} />,
      ];

  return (
    <>
      <Layout
        // The page scrolls as a whole in the AppShell's main area: a Layout
        // that fills it scrolls its content in a region Astryx does not make
        // focusable when nothing in it is.
        height="auto"
        defaultHasDividers
        header={
          <RecordHeader
            back={{ href: hostPathOf(service.host), label: service.host }}
            name={qualified(service)}
            about={[]}
            actions={actions}
          >
            {readOnly && <Banner status="info" title={t("service.detail.readOnly")} />}
            {show.error !== undefined && <ErrorMessage error={show.error} />}
            {remove.error !== undefined && <ErrorMessage error={remove.error} />}
          </RecordHeader>
        }
        content={
          <LayoutContent>
            <VStack gap={6}>
              <FieldList
                label={t("host.detail.fields")}
                fields={[
                  ["host", <Link href={hostPathOf(service.host)}>{service.host}</Link>],
                  ["name", service.name],
                  ["id", service.id],
                  ["ports", service.ports.join(", ")],
                  ["created_at", formatDate(service.created_at)],
                  ["updated_at", formatDate(service.updated_at)],
                ]}
              />
              <Attributes attributes={service.attributes} />
              {isNarrow && <Note note={service.note} />}
            </VStack>
          </LayoutContent>
        }
        end={
          isNarrow ? undefined : (
            <LayoutPanel width={320} padding={4} role="region" label={t("host.note")}>
              <Note note={service.note} />
            </LayoutPanel>
          )
        }
      />
      {action === "edit" && (
        <EditService
          service={service}
          onDone={() => {
            close();
            show.reload();
          }}
          onClose={close}
        />
      )}
      {action === "rename" && <RenameService service={service} onClose={close} />}
      {action === "remove" && (
        <ConfirmDialog
          title={t("service.remove.title", { name: qualified(service) })}
          consequence={t("service.remove.consequence", { name: qualified(service) })}
          confirm={t("service.remove.confirm")}
          onCancel={close}
          onConfirm={() => {
            close();
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
function EditService({ service, onDone, onClose }: { service: Service; onDone: () => void; onClose: () => void }) {
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
      onClose={onClose}
    />
  );
}

/** RenameService gives a Service a new name on its Host, and the page moves to it. */
function RenameService({ service, onClose }: { service: Service; onClose: () => void }) {
  const t = useT();
  const [newName, setNewName] = useState("");
  const rename = useWrite(ServiceRename);
  return (
    <FormDialog
      title={t("service.rename.title", { name: qualified(service) })}
      submit={t("service.rename.submit")}
      ready={newName !== "" && !rename.pending}
      error={rename.error}
      onClose={onClose}
      onSubmit={() =>
        void rename.submit({ host: service.host, name: service.name, new_name: newName }).then((renamed) => {
          // The old name names no Service now, so it leaves no history entry.
          if (renamed) {
            navigate(servicePath(renamed.service.host, renamed.service.name), { replace: true });
          }
        })
      }
    >
      <Field label={t("service.rename.name")} value={newName} onChange={setNewName} />
    </FormDialog>
  );
}
