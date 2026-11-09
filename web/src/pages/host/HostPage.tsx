import "./host.css";

import { Button } from "@astryxdesign/core/Button";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, LayoutPanel, Section, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { List, ListItem } from "@astryxdesign/core/List";
import { Spinner } from "@astryxdesign/core/Spinner";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Plus } from "lucide-react";
import { useState } from "react";

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
import { AdaptiveDialog, FormDialog } from "../../shell/AdaptiveDialog";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { CopyLine } from "../../shell/CopyLine";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, useT } from "../../shell/i18n";
import { navigate, type PageProps } from "../../shell/router";
import { hostPath, hostPathOf, servicePath, splitPorts, validPort } from "./record";
import { type Changes, EditRecord, Field, SpaceField } from "./RecordForm";
import { Attributes, FieldList, narrow, Note, RecordHeader, StateBadge } from "./RecordView";

/** Action is the dialog open on the page, if any. */
type Action = "enroll" | "edit" | "rename" | "move" | "addService" | "retire" | "remove";

/**
 * HostPage shows a Host the caller can see, named by its name or, once it
 * is Retired, its id, as `koko host show` does, on Astryx's detail-page
 * pattern: a summary header with its name, state, Space and actions, its
 * Services as the line items, its fields and Attributes, and its Note in
 * the rail. A Registered Host's Enroll shows the `koko host enroll` line to
 * copy, since Enroll stays in the CLI. It sets, clears, renames and moves
 * the Host, which never ask, and retires and deletes it, which ask first
 * and say what follows. A Retired Host takes only changes to its
 * Attributes and Note, and moves, and its Services are read-only.
 */
export function HostPage({ params }: PageProps) {
  const t = useT();
  const named = params["host"] ?? "";
  const show = useRead(HostShow, { host: named });
  const [action, setAction] = useState<Action>();
  const retire = useWrite(HostRetire);
  const remove = useWrite(HostRemove);
  const isNarrow = useMediaQuery(narrow);
  if (!show.data) {
    return show.error !== undefined ? <ErrorMessage error={show.error} /> : <Spinner label={t("shell.loading")} />;
  }
  const { host, services } = show.data;
  const retired = host.state === "retired";
  const close = () => setAction(undefined);
  const done = () => {
    close();
    show.reload();
  };
  const button = (label: string, act: Action, variant: "primary" | "secondary" = "secondary") => (
    <Button key={act} label={label} variant={variant} onClick={() => setAction(act)} />
  );
  const actions = [
    host.state === "registered" && button(t("host.enroll.button"), "enroll", "primary"),
    button(t("host.edit.button"), "edit"),
    !retired && button(t("host.rename.button"), "rename"),
    button(t("host.move.button"), "move"),
    !retired && button(t("host.retire.button"), "retire"),
    host.host_key === null && button(t("host.remove.button"), "remove"),
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
            back={{ href: "/hosts", label: t("host.list.title") }}
            name={host.name}
            about={[
              <StateBadge key="state" state={host.state} />,
              <Text key="space" type="body">
                {t("host.detail.inSpace", { space: host.space })}
              </Text>,
            ]}
            actions={actions}
          >
            {show.error !== undefined && <ErrorMessage error={show.error} />}
            {retire.error !== undefined && <ErrorMessage error={retire.error} />}
            {remove.error !== undefined && <ErrorMessage error={remove.error} />}
          </RecordHeader>
        }
        content={
          <LayoutContent>
            <VStack gap={6}>
              <Services
                host={host}
                services={services}
                onAdd={retired ? undefined : () => setAction("addService")}
              />
              <HostFields host={host} />
              <Attributes attributes={host.attributes} />
              {isNarrow && <Note note={host.note} />}
            </VStack>
          </LayoutContent>
        }
        end={
          isNarrow ? undefined : (
            <LayoutPanel width={320} padding={4} role="region" label={t("host.note")}>
              <Note note={host.note} />
            </LayoutPanel>
          )
        }
      />
      {action === "enroll" && <EnrollHost host={host.name} onClose={close} />}
      {action === "edit" && <EditHost host={host} onDone={done} onClose={close} />}
      {action === "rename" && <RenameHost host={host} onClose={close} />}
      {action === "move" && <MoveHost host={host} onDone={done} onClose={close} />}
      {action === "addService" && <AddService host={host.id} onClose={close} />}
      {action === "retire" && (
        <ConfirmDialog
          title={t("host.retire.title", { name: host.name })}
          consequence={t("host.retire.consequence", { name: host.name, id: host.id })}
          confirm={t("host.retire.confirm")}
          onCancel={close}
          onConfirm={() => {
            close();
            void retire.submit({ host: host.id }).then((retired) => {
              // Its name no longer names it, so the page moves to its id.
              if (retired) {
                navigate(hostPath(retired.host), { replace: true });
              }
            });
          }}
        />
      )}
      {action === "remove" && (
        <ConfirmDialog
          title={t("host.remove.title", { name: host.name })}
          consequence={
            services.length === 0
              ? t("host.remove.consequenceAlone", { name: host.name })
              : t("host.remove.consequence", { name: host.name, services: services.length })
          }
          confirm={t("host.remove.confirm")}
          onCancel={close}
          onConfirm={() => {
            close();
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
      <Link href={hostPathOf(host.relay.id)}>{host.relay.name}</Link>
    );
  return (
    <FieldList
      label={t("host.detail.fields")}
      fields={[
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
      ]}
    />
  );
}

/**
 * Services are a Host's line items: each of its Services, linking to its
 * page, and Add Service, unless the Host is Retired, whose Services are
 * read-only.
 */
function Services({ host, services, onAdd }: { host: Host; services: readonly Service[]; onAdd?: () => void }) {
  const t = useT();
  return (
    <Section variant="transparent" padding={0}>
      <VStack gap={3}>
        <HStack gap={2} vAlign="center" wrap="wrap">
          <StackItem size="fill">
            <Heading level={2}>{t("host.services")}</Heading>
          </StackItem>
          {onAdd && (
            <Button
              label={t("host.service.add.open")}
              variant="secondary"
              icon={<Icon icon={Plus} size="sm" />}
              onClick={onAdd}
            />
          )}
        </HStack>
        {services.length === 0 ? (
          <Text type="body" color="secondary">
            {t("host.services.none")}
          </Text>
        ) : (
          <List density="spacious" edge="outset" hasDividers aria-label={t("host.services.of", { name: host.name })}>
            {services.map((s) => (
              <ListItem
                key={s.id}
                label={s.name}
                description={s.ports.length === 0 ? t("host.services.noPorts") : s.ports.join(", ")}
                href={servicePath(s.host, s.name)}
              />
            ))}
          </List>
        )}
      </VStack>
    </Section>
  );
}

/**
 * EnrollHost shows the `koko host enroll <host>` line a Registered Host's
 * Enroll runs on a device that has joined, with a button that copies it.
 */
function EnrollHost({ host, onClose }: { host: string; onClose: () => void }) {
  const t = useT();
  return (
    <AdaptiveDialog
      title={t("host.enroll.title", { name: host })}
      onClose={onClose}
      footer={
        <HStack hAlign="end">
          <Button label={t("host.enroll.done")} variant="secondary" onClick={onClose} />
        </HStack>
      }
    >
      <Text type="body">{t("host.enroll.hint")}</Text>
      <CopyLine line={`koko host enroll ${host}`} />
    </AdaptiveDialog>
  );
}

/** AddService records a Service on the Host whose id is host, and the page moves to it. */
function AddService({ host, onClose }: { host: string; onClose: () => void }) {
  const t = useT();
  const [name, setName] = useState("");
  const [ports, setPorts] = useState("");
  const add = useWrite(ServiceAdd);
  return (
    <FormDialog
      title={t("host.service.add.title")}
      submit={t("host.service.add.submit")}
      ready={name !== "" && !add.pending}
      error={add.error}
      onClose={onClose}
      onSubmit={() => {
        const listed = splitPorts(ports);
        void add.submit({ host, name, ...(listed.length > 0 && { ports: listed }) }).then((added) => {
          if (added) {
            navigate(servicePath(added.service.host, added.service.name));
          }
        });
      }}
    >
      <Field label="name" value={name} onChange={setName} />
      <Field label="ports" value={ports} onChange={setPorts} />
    </FormDialog>
  );
}

/** ChangeProps are what the form of a reversible action on a Host gets. */
interface ChangeProps {
  host: Host;
  /** onDone hears that the write succeeded. */
  onDone: () => void;
  onClose: () => void;
}

/**
 * EditHost sets and clears a Host's address, port, Host Account, Relay,
 * Attributes and Note, sending only those changed; a Retired Host's form
 * offers only its Attributes and Note.
 */
function EditHost({ host, onDone, onClose }: ChangeProps) {
  const t = useT();
  const set = useWrite(HostSet);
  const own =
    host.state === "retired"
      ? []
      : [
          { key: "address", value: host.address, clearable: false },
          { key: "port", value: String(host.port), clearable: true, valid: validPort, inputMode: "numeric" as const },
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
      onClose={onClose}
    />
  );
}

/** RenameHost gives a Host a new name, its SSH Name, and the page moves to it. */
function RenameHost({ host, onClose }: Omit<ChangeProps, "onDone">) {
  const t = useT();
  const [newName, setNewName] = useState("");
  const rename = useWrite(HostRename);
  return (
    <FormDialog
      title={t("host.rename.title", { name: host.name })}
      submit={t("host.rename.submit")}
      ready={newName !== "" && !rename.pending}
      error={rename.error}
      onClose={onClose}
      onSubmit={() =>
        void rename.submit({ host: host.id, new_name: newName }).then((renamed) => {
          // The old name names no Host now, so it leaves no history entry.
          if (renamed) {
            navigate(hostPath(renamed.host), { replace: true });
          }
        })
      }
    >
      <Field label={t("host.rename.name")} value={newName} onChange={setNewName} />
    </FormDialog>
  );
}

/** MoveHost moves a Host, under its name, to another Space the caller can act in. */
function MoveHost({ host, onDone, onClose }: ChangeProps) {
  const t = useT();
  const spaces = useRead(SpaceList, {});
  const [space, setSpace] = useState("");
  const move = useWrite(HostMove);
  return (
    <FormDialog
      title={t("host.move.title", { name: host.name })}
      submit={t("host.move.submit")}
      ready={space !== "" && !move.pending}
      error={move.error ?? spaces.error}
      onClose={onClose}
      onSubmit={() => void move.submit({ host: host.id, space }).then((moved) => moved && onDone())}
    >
      <SpaceField
        label={t("host.move.space")}
        spaces={(spaces.data?.spaces ?? []).map((s) => s.name).filter((s) => s !== host.space)}
        value={space}
        onChange={setSpace}
      />
    </FormDialog>
  );
}
