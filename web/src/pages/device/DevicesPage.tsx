import { Badge } from "@astryxdesign/core/Badge";
import { Button } from "@astryxdesign/core/Button";
import { DropdownMenu } from "@astryxdesign/core/DropdownMenu";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { Switch } from "@astryxdesign/core/Switch";
import { proportional, Table, type TableColumn, useTableStickyColumns } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { VisuallyHidden } from "@astryxdesign/core/VisuallyHidden";
import { Ellipsis, Plus } from "lucide-react";
import { useState } from "react";

import { DeviceAdd, DeviceList, DeviceRevoke, type DeviceSession } from "../../api/device";
import { UserList } from "../../api/user";
import { type Caller, useCaller } from "../../shell/caller";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, type Translate, useT } from "../../shell/i18n";
import { useQueryFlag } from "../../shell/router";
import { useShownOnce } from "../../shell/shownOnce";
import { type Issued, IssueWizard } from "./IssueWizard";

/**
 * dateWidth is a date column's: never narrower than a date, so on a phone
 * the table scrolls sideways in its own box rather than break one.
 */
const dateWidth = proportional(1, { minWidth: 112 });
/** SessionRow is a Device Session as a row of the table. */
interface SessionRow extends Record<string, unknown> {
  id: string;
  session: DeviceSession;
}

/**
 * DevicesPage lists the caller's own Device Sessions, or everyone's for the
 * Owner, the Revoked ones too when asked (?all=1, as `koko device ls --all`).
 * A live one is revoked, from its row's menu, once the caller confirms, and
 * the Owner's own last one only once its name is typed: the web UI's form of
 * --last. Add device is a wizard: whose device and its name, then its Join
 * Code, shown once; issuing never asks.
 */
export function DevicesPage() {
  const t = useT();
  const caller = useCaller();
  const [all, showRevoked] = useQueryFlag("all");
  const list = useRead(DeviceList, all ? { all: true } : {});
  const revoke = useWrite(DeviceRevoke);
  const [revoking, setRevoking] = useState<DeviceSession>();
  const [adding, setAdding] = useState(false);
  const sessions = list.data?.device_sessions ?? [];
  const ownersLive = sessions.filter((s) => caller.owner && s.user === caller.user.name && !s.revoked_at);
  const ownersLast = (s: DeviceSession) => ownersLive.length === 1 && ownersLive[0]?.id === s.id;

  // On a phone the table scrolls sideways in its own box, its name in view.
  const sticky = useTableStickyColumns<SessionRow>({ startKeys: caller.owner ? ["user", "device"] : ["device"] });
  const columns: TableColumn<SessionRow>[] = [
    ...(caller.owner
      ? [{ key: "user", header: t("device.column.user"), renderCell: ({ session }: SessionRow) => session.user }]
      : []),
    {
      key: "device",
      header: t("device.column.device"),
      renderCell: ({ session }) =>
        session.id === caller.deviceSession?.id ? (
          <VStack gap={0}>
            {session.device}
            <Text type="supporting" color="secondary">
              {t("device.here")}
            </Text>
          </VStack>
        ) : (
          session.device
        ),
    },
    {
      key: "addedBy",
      header: t("device.column.addedBy"),
      renderCell: ({ session }) =>
        session.added_by ? `${session.added_by.user}/${session.added_by.device}` : t("device.addedBy.socket"),
    },
    {
      key: "joined",
      header: t("device.column.joined"),
      width: dateWidth,
      renderCell: ({ session }) => formatDate(session.created_at),
    },
    {
      key: "lastUsed",
      header: t("device.column.lastUsed"),
      width: dateWidth,
      renderCell: ({ session }) =>
        session.last_used_at ? formatDate(session.last_used_at) : t("device.lastUsed.never"),
    },
    ...(all
      ? [
          {
            key: "revoked",
            header: t("device.column.revoked"),
            width: dateWidth,
            renderCell: ({ session }: SessionRow) => (session.revoked_at ? formatDate(session.revoked_at) : ""),
          },
        ]
      : []),
    {
      key: "actions",
      header: <VisuallyHidden>{t("device.column.actions")}</VisuallyHidden>,
      align: "end",
      renderCell: ({ session }) =>
        !session.revoked_at && (
          <DropdownMenu
            alignment="end"
            presentation="adaptive"
            renderTrigger={(trigger) => (
              <IconButton
                {...trigger}
                variant="ghost"
                size="sm"
                label={t("device.actions.label", { name: shownName(session, caller) })}
                tooltip={t("device.actions.label", { name: shownName(session, caller) })}
                icon={<Icon icon={Ellipsis} size="sm" />}
              />
            )}
            items={[{ label: t("device.revoke.button"), variant: "destructive", onClick: () => setRevoking(session) }]}
          />
        ),
    },
  ];

  return (
    <Layout
      contentWidth={1080}
      content={
        <LayoutContent>
          <VStack gap={6}>
            <HStack gap={3} vAlign="center" wrap="wrap">
              <StackItem size="fill">
                <HStack gap={2} vAlign="center">
                  <Heading level={1}>{t("device.list.title")}</Heading>
                  {list.data && <Badge variant="neutral" label={String(sessions.length)} />}
                </HStack>
              </StackItem>
              <Button
                variant="primary"
                label={t("device.add.button")}
                icon={<Icon icon={Plus} size="sm" />}
                onClick={() => setAdding(true)}
              />
            </HStack>
            <Switch label={t("device.list.showRevoked")} value={all} onChange={showRevoked} />
            {list.error !== undefined && <ErrorMessage error={list.error} />}
            {revoke.error !== undefined && <ErrorMessage error={revoke.error} />}
            {list.data && (
              <Table<SessionRow>
                aria-label={t("device.list.title")}
                data={sessions.map((session) => ({ id: session.id, session }))}
                columns={columns}
                idKey="id"
                plugins={{ sticky }}
                hasHover
                textOverflow="wrap"
              />
            )}
          </VStack>
          {revoking && (
            <ConfirmDialog
              title={t("device.revoke.title", { name: shownName(revoking, caller) })}
              consequence={revokeConsequence(t, revoking, caller, ownersLast(revoking))}
              confirm={t("device.revoke.confirm")}
              typedName={ownersLast(revoking) ? revoking.device : undefined}
              onCancel={() => setRevoking(undefined)}
              onConfirm={() => {
                setRevoking(undefined);
                const last = ownersLast(revoking) || undefined;
                // A refusal reloads the list too: one out of date, such as
                // one still showing a second device of the Owner's, is put
                // right.
                void revoke.submit({ user: revoking.user, device: revoking.device, last }).then(list.reload);
              }}
            />
          )}
          {adding && <AddDevice onClose={() => setAdding(false)} onAdded={list.reload} />}
        </LayoutContent>
      }
    />
  );
}

/**
 * shownName names a Device Session as the CLI does: bare for one of the
 * caller's own devices, and <user>/<device> for another User's.
 */
function shownName(session: DeviceSession, caller: Caller): string {
  return session.user === caller.user.name ? session.device : `${session.user}/${session.device}`;
}

/**
 * revokeConsequence states what revoking session does: for the Owner's last
 * device, that only the koko server's own machine adds another; for the
 * device this page runs through, that the page stops; for one that is both,
 * both; else that the device is cut off.
 */
function revokeConsequence(t: Translate, session: DeviceSession, caller: Caller, ownersLast: boolean): string {
  const name = shownName(session, caller);
  const here = session.id === caller.deviceSession?.id;
  if (ownersLast) {
    return here ? t("device.revoke.consequenceLastHere", { name }) : t("device.revoke.consequenceLast", { name });
  }
  return here ? t("device.revoke.consequenceHere", { name }) : t("device.revoke.consequence", { name });
}

/**
 * AddDevice is the wizard that issues a Join Code for a new device: the
 * caller's own, or, for the Owner, any User's who is not Removed, with an
 * optional name; then the code, shown once. It never asks.
 */
function AddDevice({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const t = useT();
  const caller = useCaller();
  const [device, setDevice] = useState("");
  const [user, setUser] = useState(caller.user.name);
  const [issued, setIssued] = useShownOnce<Issued>();
  const add = useWrite(DeviceAdd);
  return (
    <IssueWizard
      title={t("device.add.title")}
      step={t("device.add.step")}
      submit={t("device.add.submit")}
      ready={!add.pending}
      error={add.error}
      issued={issued}
      onSubmit={() =>
        void add.submit({ device: device || undefined, user: caller.owner ? user : undefined }).then((added) => {
          if (added) {
            setIssued({ joinCode: added.join_code });
            onAdded();
          }
        })
      }
      onClose={() => {
        setIssued(undefined);
        onClose();
      }}
    >
      {caller.owner && <UserChoice user={user} onChange={setUser} />}
      <TextInput
        label={t("device.add.name")}
        description={t("device.add.nameHint")}
        isOptional
        value={device}
        onChange={setDevice}
        autoComplete="off"
        autoCapitalize="off"
        hasAutoCorrect={false}
        hasSpellCheck={false}
      />
    </IssueWizard>
  );
}

/** UserChoice is the Owner's choice of whose device to add, among the Users who are not Removed. */
function UserChoice({ user, onChange }: { user: string; onChange: (user: string) => void }) {
  const t = useT();
  const users = useRead(UserList, {});
  return (
    <>
      <Selector
        label={t("device.add.user")}
        options={users.data?.users.map((u) => u.name) ?? [user]}
        value={user}
        onChange={onChange}
        isLoading={!users.data && users.error === undefined}
        presentation="adaptive"
      />
      {users.error !== undefined && <ErrorMessage error={users.error} />}
    </>
  );
}
