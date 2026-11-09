import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { DropdownMenu } from "@astryxdesign/core/DropdownMenu";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Switch } from "@astryxdesign/core/Switch";
import { proportional, Table, type TableColumn, useTableStickyColumns } from "@astryxdesign/core/Table";
import { Heading } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { VisuallyHidden } from "@astryxdesign/core/VisuallyHidden";
import { Ellipsis, UserPlus } from "lucide-react";
import { useState } from "react";

import { CodeSpaceNotEmpty } from "../../api/space";
import { type User, UserInvite, UserList, UserRemove, type UserRemoveResponse } from "../../api/user";
import { ApiError } from "../../client";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, type Translate, useT } from "../../shell/i18n";
import { useQueryFlag } from "../../shell/router";
import { useShownOnce } from "../../shell/shownOnce";
import { type Issued, IssueWizard } from "../device/IssueWizard";

/**
 * dateWidth is a date column's: never narrower than a date, so on a phone
 * the table scrolls sideways in its own box rather than break one.
 */
const dateWidth = proportional(1, { minWidth: 112 });
/** UserRow is a User as a row of the table. */
interface UserRow extends Record<string, unknown> {
  id: string;
  user: User;
}

/**
 * PeoplePage, the Owner's alone, lists the Users, the Removed ones too when
 * asked (?all=1, as `koko user ls --all`). Invite is a wizard: the new
 * User's name, then their Invite, their first Join Code, shown once. A User
 * is removed, from their row's menu, once the Owner confirms; the page then
 * says whether the User was deleted or Removed, from the answer.
 */
export function PeoplePage() {
  const t = useT();
  const [all, showRemoved] = useQueryFlag("all");
  const list = useRead(UserList, all ? { all: true } : {});
  const remove = useWrite(UserRemove);
  const [asking, setAsking] = useState<User>();
  const [inviting, setInviting] = useState(false);
  // removing is the User the last removal was for, and removed its answer.
  const [removing, setRemoving] = useState<string>();
  const [removed, setRemoved] = useState<UserRemoveResponse>();
  const users = list.data?.users ?? [];

  // On a phone the table scrolls sideways in its own box, its name in view.
  const sticky = useTableStickyColumns<UserRow>({ startKeys: ["name"] });
  const columns: TableColumn<UserRow>[] = [
    { key: "name", header: t("user.column.name"), renderCell: ({ user }) => user.name },
    { key: "role", header: t("user.column.role"), renderCell: ({ user }) => roleName(t, user.role) },
    {
      key: "created",
      header: t("user.column.created"),
      width: dateWidth,
      renderCell: ({ user }) => formatDate(user.created_at),
    },
    ...(all
      ? [
          {
            key: "removed",
            header: t("user.column.removed"),
            width: dateWidth,
            renderCell: ({ user }: UserRow) => (user.removed_at ? formatDate(user.removed_at) : ""),
          },
        ]
      : []),
    {
      key: "actions",
      header: <VisuallyHidden>{t("user.column.actions")}</VisuallyHidden>,
      align: "end",
      renderCell: ({ user }) =>
        user.role !== "owner" &&
        !user.removed_at && (
          <DropdownMenu
            alignment="end"
            presentation="adaptive"
            renderTrigger={(trigger) => (
              <IconButton
                {...trigger}
                variant="ghost"
                size="sm"
                label={t("user.actions.label", { name: user.name })}
                tooltip={t("user.actions.label", { name: user.name })}
                icon={<Icon icon={Ellipsis} size="sm" />}
              />
            )}
            items={[{ label: t("user.remove.button"), variant: "destructive", onClick: () => setAsking(user) }]}
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
                  <Heading level={1}>{t("user.list.title")}</Heading>
                  {list.data && <Badge variant="neutral" label={String(users.length)} />}
                </HStack>
              </StackItem>
              <Button
                variant="primary"
                label={t("user.invite.button")}
                icon={<Icon icon={UserPlus} size="sm" />}
                onClick={() => setInviting(true)}
              />
            </HStack>
            <Switch label={t("user.list.showRemoved")} value={all} onChange={showRemoved} />
            {list.error !== undefined && <ErrorMessage error={list.error} />}
            {removed && <Banner status="success" role="status" title={removalMessage(t, removed)} />}
            {remove.error !== undefined && removing !== undefined && (
              <RemovalRefused error={remove.error} name={removing} />
            )}
            {list.data && (
              <Table<UserRow>
                aria-label={t("user.list.title")}
                data={users.map((user) => ({ id: user.id, user }))}
                columns={columns}
                idKey="id"
                plugins={{ sticky }}
                hasHover
                textOverflow="wrap"
              />
            )}
          </VStack>
          {asking && (
            <ConfirmDialog
              title={t("user.remove.title", { name: asking.name })}
              consequence={t("user.remove.consequence", { name: asking.name })}
              confirm={t("user.remove.confirm")}
              onCancel={() => setAsking(undefined)}
              onConfirm={() => {
                setAsking(undefined);
                setRemoving(asking.name);
                setRemoved(undefined);
                void remove.submit({ name: asking.name }).then((answer) => {
                  setRemoved(answer);
                  list.reload();
                });
              }}
            />
          )}
          {inviting && <InviteUser onClose={() => setInviting(false)} onInvited={list.reload} />}
        </LayoutContent>
      }
    />
  );
}

/** roleName is a User's role for people: Owner or Member, or the role as stored if neither. */
function roleName(t: Translate, role: string): string {
  return role === "owner" ? t("user.role.owner") : role === "member" ? t("user.role.member") : role;
}

/** removalMessage says what user.remove did, from its outcome. */
function removalMessage(t: Translate, { outcome, user }: UserRemoveResponse): string {
  if (outcome === "deleted") {
    return t("user.removed.deleted", { name: user.name });
  }
  if (outcome === "removed") {
    return t("user.removed.removed", { name: user.name });
  }
  return t("shell.error.unexpected"); // an outcome this web UI does not know says nothing it can trust
}

/**
 * RemovalRefused says why removing the User name was refused. A User who
 * never joined is deleted with their Personal Space, which must be empty, so
 * space_not_empty names that Space here, where the page shows none.
 */
function RemovalRefused({ error, name }: { error: unknown; name: string }) {
  const t = useT();
  if (error instanceof ApiError && error.code === CodeSpaceNotEmpty) {
    return <Banner status="error" role="alert" title={t("user.remove.spaceNotEmpty", { name })} />;
  }
  return <ErrorMessage error={error} />;
}

/**
 * InviteUser is the Owner's wizard for a new User, a Member at once: their
 * name, then their Invite, their first Join Code, shown once. It never asks.
 */
function InviteUser({ onClose, onInvited }: { onClose: () => void; onInvited: () => void }) {
  const t = useT();
  const [name, setName] = useState("");
  const [issued, setIssued] = useShownOnce<Issued>();
  const invite = useWrite(UserInvite);
  return (
    <IssueWizard
      title={t("user.invite.title")}
      step={t("user.invite.step")}
      submit={t("user.invite.submit")}
      ready={name !== "" && !invite.pending}
      error={invite.error}
      issued={issued}
      onSubmit={() =>
        void invite.submit({ name }).then((answer) => {
          if (answer) {
            setIssued({ joinCode: answer.join_code, said: t("user.invite.done", { name: answer.user.name }) });
            onInvited();
          }
        })
      }
      onClose={() => {
        setIssued(undefined);
        onClose();
      }}
    >
      <TextInput
        label={t("user.invite.name")}
        description={t("user.invite.nameHint")}
        value={name}
        onChange={setName}
        autoComplete="off"
        autoCapitalize="off"
        hasAutoCorrect={false}
        hasSpellCheck={false}
      />
    </IssueWizard>
  );
}
