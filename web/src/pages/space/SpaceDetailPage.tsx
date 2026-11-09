import { Badge } from "@astryxdesign/core/Badge";
import { Button } from "@astryxdesign/core/Button";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { List, ListItem } from "@astryxdesign/core/List";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Selector } from "@astryxdesign/core/Selector";
import { Spinner } from "@astryxdesign/core/Spinner";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { ArrowLeft, Plus } from "lucide-react";
import { useId, useState } from "react";

import {
  type SpaceDetail,
  SpaceMemberAdd,
  SpaceMemberRemove,
  SpaceRemove,
  SpaceRename,
  SpaceShow,
} from "../../api/space";
import { UserList } from "../../api/user";
import { FormDialog } from "../../shell/AdaptiveDialog";
import { useCaller } from "../../shell/caller";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { ErrorMessage, useT } from "../../shell/i18n";
import { navigate, type PageProps } from "../../shell/router";
import { spacePath } from "./path";

/** Asking is the dialog open on a Space's page, if any. */
type Asking = "rename" | "remove" | "addMember";

/**
 * SpaceDetailPage shows a Space the caller can act in: whether it is a
 * Personal Space and how many records it holds, in its header, and its
 * Members as its line items. The Owner renames and removes a shared Space
 * here and changes its Members; a Personal Space offers none of these, as
 * the koko server refuses them while its User exists, and a Member sees
 * none of them.
 */
export function SpaceDetailPage({ params }: PageProps) {
  const t = useT();
  const show = useRead(SpaceShow, { name: params["name"] ?? "" });
  const mayChange = useCaller().owner && show.data?.space.personal === false;
  const [asking, setAsking] = useState<Asking>();
  const remove = useWrite(SpaceRemove);
  const back = (
    <Link href="/spaces" color="secondary">
      <HStack gap={1} vAlign="center">
        <Icon icon={ArrowLeft} size="sm" color="inherit" />
        {t("space.detail.back")}
      </HStack>
    </Link>
  );
  if (!show.data) {
    return (
      <Layout
        contentWidth={1080}
        content={
          <LayoutContent>
            <VStack gap={4}>
              {back}
              {show.error !== undefined ? (
                <ErrorMessage error={show.error} />
              ) : (
                <Spinner label={t("space.detail.loading")} />
              )}
            </VStack>
          </LayoutContent>
        }
      />
    );
  }
  const { space } = show.data;
  const stop = () => setAsking(undefined);
  return (
    <Layout
      contentWidth={1080}
      header={
        <LayoutHeader hasDivider>
          <VStack gap={3}>
            {back}
            <HStack gap={4} vAlign="start" wrap="wrap">
              <StackItem size="fill">
                <VStack gap={2}>
                  <VStack gap={1}>
                    <Heading level={1}>{space.name}</Heading>
                    <Text type="body" color="secondary">
                      {space.personal ? t("space.personal") : t("space.shared")}
                    </Text>
                  </VStack>
                  <MetadataList orientation="horizontal">
                    <MetadataListItem label={t("space.counts.hosts")}>{space.counts.hosts}</MetadataListItem>
                    <MetadataListItem label={t("space.counts.retiredHosts")}>
                      {space.counts.retired_hosts}
                    </MetadataListItem>
                    <MetadataListItem label={t("space.counts.secrets")}>{space.counts.secrets}</MetadataListItem>
                  </MetadataList>
                </VStack>
              </StackItem>
              {mayChange && (
                <HStack gap={2} wrap="wrap">
                  <Button variant="secondary" label={t("space.rename.button")} onClick={() => setAsking("rename")} />
                  <Button variant="secondary" label={t("space.remove.button")} onClick={() => setAsking("remove")} />
                </HStack>
              )}
            </HStack>
            {show.error !== undefined && <ErrorMessage error={show.error} />}
            {remove.error !== undefined && <ErrorMessage error={remove.error} />}
          </VStack>
        </LayoutHeader>
      }
      content={
        <LayoutContent>
          <Members space={space} mayChange={mayChange} onAdd={() => setAsking("addMember")} onChanged={show.reload} />
          {asking === "rename" && (
            <RenameSpace
              name={space.name}
              onRenamed={(newName) => {
                // The page stays mounted as it moves, so the dialog closes first;
                // the old name names no Space now, so it leaves no history entry.
                stop();
                navigate(spacePath(newName), { replace: true });
              }}
              onCancel={stop}
            />
          )}
          {asking === "remove" && (
            <ConfirmDialog
              title={t("space.remove.title", { name: space.name })}
              consequence={t("space.remove.consequence", { name: space.name })}
              confirm={t("space.remove.confirm")}
              onCancel={stop}
              onConfirm={() => {
                stop();
                void remove.submit({ name: space.name }).then((removed) => {
                  if (removed) {
                    navigate("/spaces", { replace: true });
                  }
                });
              }}
            />
          )}
          {asking === "addMember" && (
            <AddMember
              space={space}
              onAdded={() => {
                stop();
                show.reload();
              }}
              onCancel={stop}
            />
          )}
        </LayoutContent>
      }
    />
  );
}

/**
 * Members lists a Space's Members as its line items. For a shared Space the
 * Owner takes a Member out and adds one; both are reversible writes and
 * never ask.
 */
function Members({
  space,
  mayChange,
  onAdd,
  onChanged,
}: {
  space: SpaceDetail;
  mayChange: boolean;
  onAdd: () => void;
  onChanged: () => void;
}) {
  const t = useT();
  const titleId = useId();
  const memberRemove = useWrite(SpaceMemberRemove);
  const takeOut = (user: string) =>
    void memberRemove.submit({ space: space.name, users: [user] }).then((done) => done && onChanged());
  return (
    <VStack gap={3}>
      <HStack gap={2} vAlign="center" wrap="wrap">
        <StackItem size="fill">
          <HStack gap={2} vAlign="center">
            <Heading level={2} id={titleId}>
              {t("space.detail.members")}
            </Heading>
            <Badge variant="neutral" label={String(space.members.length)} />
          </HStack>
        </StackItem>
        {mayChange && (
          <Button
            variant="secondary"
            label={t("space.member.add.button")}
            icon={<Icon icon={Plus} size="sm" />}
            onClick={onAdd}
          />
        )}
      </HStack>
      {memberRemove.error !== undefined && <ErrorMessage error={memberRemove.error} />}
      {space.members.length === 0 ? (
        <Text type="body" color="secondary">
          {t("space.detail.noMembers")}
        </Text>
      ) : (
        <List aria-labelledby={titleId} hasDividers edge="flush">
          {space.members.map((member) => (
            <ListItem
              key={member.id}
              label={member.name}
              endContent={
                mayChange && (
                  <Button
                    variant="ghost"
                    size="sm"
                    label={t("space.member.remove")}
                    aria-label={t("space.member.remove.label", { user: member.name, space: space.name })}
                    onClick={() => takeOut(member.name)}
                  />
                )
              }
            />
          ))}
        </List>
      )}
    </VStack>
  );
}

/** RenameSpace is the Owner's dialog for a shared Space's new name; the page moves to it. */
function RenameSpace({
  name,
  onRenamed,
  onCancel,
}: {
  name: string;
  onRenamed: (newName: string) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [newName, setNewName] = useState("");
  const rename = useWrite(SpaceRename);
  return (
    <FormDialog
      title={t("space.rename.title", { name })}
      submit={t("space.rename.submit")}
      ready={newName !== "" && !rename.pending}
      error={rename.error}
      onSubmit={() =>
        void rename.submit({ name, new_name: newName }).then((renamed) => renamed && onRenamed(renamed.space.name))
      }
      onClose={onCancel}
    >
      <TextInput
        label={t("space.rename.name")}
        value={newName}
        onChange={setNewName}
        autoComplete="off"
        autoCapitalize="off"
        hasAutoCorrect={false}
        hasSpellCheck={false}
      />
    </FormDialog>
  );
}

/** AddMember is the Owner's dialog to make a User a Member, chosen among the Users who are not one. */
function AddMember({ space, onAdded, onCancel }: { space: SpaceDetail; onAdded: () => void; onCancel: () => void }) {
  const t = useT();
  const users = useRead(UserList, {});
  const [user, setUser] = useState("");
  const add = useWrite(SpaceMemberAdd);
  const choices = (users.data?.users ?? []).filter((u) => !space.members.some((m) => m.id === u.id));
  return (
    <FormDialog
      title={t("space.member.add.title", { space: space.name })}
      submit={t("space.member.add.submit")}
      ready={user !== "" && !add.pending}
      error={add.error ?? users.error}
      onSubmit={() => void add.submit({ space: space.name, users: [user] }).then((added) => added && onAdded())}
      onClose={onCancel}
    >
      <Selector
        label={t("space.member.add.user")}
        placeholder={t("space.member.add.choose")}
        emptyText={t("space.member.add.none")}
        options={choices.map((u) => u.name)}
        value={user}
        onChange={setUser}
        isLoading={!users.data && users.error === undefined}
        presentation="adaptive"
      />
    </FormDialog>
  );
}
