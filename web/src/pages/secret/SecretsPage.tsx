import "./secret.css";

import { Badge } from "@astryxdesign/core/Badge";
import { Button } from "@astryxdesign/core/Button";
import { DropdownMenu } from "@astryxdesign/core/DropdownMenu";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { Spinner } from "@astryxdesign/core/Spinner";
import { proportional, Table, type TableColumn, useTableStickyColumns } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextArea } from "@astryxdesign/core/TextArea";
import { TextInput } from "@astryxdesign/core/TextInput";
import { VisuallyHidden } from "@astryxdesign/core/VisuallyHidden";
import { Ellipsis, KeyRound, Plus, Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import {
  type Secret,
  SecretAdd,
  SecretList,
  SecretMove,
  SecretRemove,
  SecretRename,
  SecretSet,
  SecretShow,
} from "../../api/secret";
import { SpaceList } from "../../api/space";
import { call } from "../../client";
import { AdaptiveDialog, FormDialog } from "../../shell/AdaptiveDialog";
import { useCaller } from "../../shell/caller";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { CopyLine } from "../../shell/CopyLine";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, useT } from "../../shell/i18n";
import { useSearchParams } from "../../shell/router";
import { useShownOnce } from "../../shell/shownOnce";
import { encodeValue, type ShownValue, showValue } from "./value";

/**
 * dateWidth is a date column's: never narrower than a date, so on a phone
 * the table scrolls sideways in its own box rather than break one.
 */
const dateWidth = proportional(1, { minWidth: 112 });
/** revealFor is how long a revealed value stays on screen: 30 seconds (P0 spec: Secrets). */
const revealFor = 30_000;

/**
 * Reveal is the one Secret whose value is asked for or on screen: the
 * value once secret.show answered it, or why it refused.
 */
interface Reveal {
  secret: Secret;
  value?: ShownValue;
  error?: unknown;
}

/** Change is a write a person is filling in for one Secret. */
interface Change {
  secret: Secret;
  action: "set" | "rename" | "move" | "remove";
}

/** SecretRow is a Secret as a row of the table. */
interface SecretRow extends Record<string, unknown> {
  id: string;
  secret: Secret;
}

/** qualified names a Secret as every command does: <space>/<secret>. */
function qualified(secret: Secret): string {
  return `${secret.space}/${secret.name}`;
}

/**
 * SecretsPage lists the names of the Secrets in every Space the caller can
 * act in, as `koko secret ls` does, searched by name in place (?q= in the
 * URL), and never a value until one is asked for: Show calls secret.show
 * and the value shows in a dialog, with a button that copies it, until the
 * dialog closes, 30 seconds pass or the page is left. A value lives only in
 * the page's state, never in the URL or the browser's storage, and the web
 * front sends it with Cache-Control: no-store. The page adds, sets,
 * renames, moves and deletes Secrets, each in a dialog; only deleting asks
 * first.
 */
export function SecretsPage() {
  const t = useT();
  const list = useRead(SecretList, {});
  const spaces = useRead(SpaceList, {});
  const [params, setParams] = useSearchParams();
  const query = params.get("q") ?? "";
  const [reveal, setReveal] = useShownOnce<Reveal>();
  const [changing, setChanging] = useState<Change>();
  const [adding, setAdding] = useState(false);
  const remove = useWrite(SecretRemove);
  const secrets = list.data?.secrets ?? [];
  const spaceNames = spaces.data?.spaces.map((s) => s.name);
  const term = query.trim().toLowerCase();
  const shown = term === "" ? secrets : secrets.filter((s) => qualified(s).toLowerCase().includes(term));
  // asking is the latest reveal, a new object for each reveal and each
  // hide, with the id of the Secret asked for. A value secret.show answers
  // after a later reveal, a hide, a write to its Secret or the browser
  // leaving the page is not asking's, and never shows: it may be out of
  // date, or no longer wanted.
  const asking = useRef<{ id?: string }>({});

  const value = reveal?.value;
  useEffect(() => {
    if (!value) {
      return;
    }
    const hide = setTimeout(() => {
      asking.current = {};
      setReveal(undefined);
    }, revealFor);
    return () => clearTimeout(hide);
  }, [value, setReveal]);

  useEffect(() => {
    const left = () => {
      asking.current = {};
    };
    window.addEventListener("pagehide", left);
    return () => window.removeEventListener("pagehide", left);
  }, []);

  const search = (next: string) => {
    const nextParams = new URLSearchParams(params);
    if (next === "") {
      nextParams.delete("q");
    } else {
      nextParams.set("q", next);
    }
    // Each keystroke refines the view in place rather than adding a visit.
    setParams(nextParams, { replace: true });
  };
  const hide = () => {
    asking.current = {};
    setReveal(undefined);
  };
  // secret.show is a read the person asks for, so it is called here, on
  // the click, rather than through useRead, which reads as the page loads.
  const show = (secret: Secret) => {
    const ask = { id: secret.id };
    asking.current = ask;
    setReveal({ secret });
    call(SecretShow, { space: secret.space, name: secret.name }).then(
      (answer) => ask === asking.current && setReveal({ secret, value: showValue(answer.value_base64) }),
      (error: unknown) => ask === asking.current && setReveal({ secret, error }),
    );
  };
  // A Secret written to shows its value no more: it may be out of date.
  const changed = (secret: Secret) => {
    if (reveal?.secret.id === secret.id || asking.current.id === secret.id) {
      hide();
    }
    setChanging(undefined);
    list.reload();
  };
  const stopChanging = () => setChanging(undefined);

  // On a phone the table scrolls sideways in its own box, its name in view.
  const sticky = useTableStickyColumns<SecretRow>({ startKeys: ["space", "name"] });
  const columns: TableColumn<SecretRow>[] = [
    { key: "space", header: t("secret.column.space"), renderCell: ({ secret }) => secret.space },
    { key: "name", header: t("secret.column.name"), renderCell: ({ secret }) => secret.name },
    {
      key: "updated",
      header: t("secret.column.updated"),
      width: dateWidth,
      renderCell: ({ secret }) => formatDate(secret.updated_at),
    },
    {
      key: "value",
      header: t("secret.column.value"),
      renderCell: ({ secret }) => (
        <Button
          variant="secondary"
          size="sm"
          label={t("secret.show.button")}
          aria-label={t("secret.show.label", { name: qualified(secret) })}
          onClick={() => show(secret)}
        />
      ),
    },
    {
      key: "actions",
      header: <VisuallyHidden>{t("secret.column.actions")}</VisuallyHidden>,
      align: "end",
      renderCell: ({ secret }) => (
        <RowActions name={qualified(secret)} onChoose={(action) => setChanging({ secret, action })} />
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
                  <Heading level={1}>{t("secret.list.title")}</Heading>
                  {list.data && <Badge variant="neutral" label={String(secrets.length)} />}
                </HStack>
              </StackItem>
              <Button
                variant="primary"
                label={t("secret.add.button")}
                icon={<Icon icon={Plus} size="sm" />}
                onClick={() => setAdding(true)}
              />
            </HStack>
            {list.error !== undefined && <ErrorMessage error={list.error} />}
            {remove.error !== undefined && <ErrorMessage error={remove.error} />}
            {list.data && secrets.length === 0 && (
              <EmptyState
                headingLevel={2}
                icon={<Icon icon={KeyRound} size="lg" />}
                title={t("secret.list.empty")}
                description={t("secret.list.emptyHint")}
              />
            )}
            {secrets.length > 0 && (
              <VStack gap={4}>
                <TextInput
                  type="search"
                  label={t("secret.search.label")}
                  isLabelHidden
                  placeholder={t("secret.search.placeholder")}
                  startIcon={<Icon icon={Search} size="sm" />}
                  value={query}
                  onChange={search}
                  autoComplete="off"
                  hasSpellCheck={false}
                />
                {shown.length === 0 ? (
                  <EmptyState
                    headingLevel={2}
                    icon={<Icon icon={Search} size="lg" />}
                    title={t("secret.search.none")}
                    description={t("secret.search.noneHint", { query: query.trim() })}
                    actions={<Button variant="secondary" label={t("secret.search.clear")} onClick={() => search("")} />}
                  />
                ) : (
                  <Table<SecretRow>
                    aria-label={t("secret.list.title")}
                    data={shown.map((secret) => ({ id: secret.id, secret }))}
                    columns={columns}
                    idKey="id"
                    plugins={{ sticky }}
                    hasHover
                    textOverflow="wrap"
                  />
                )}
              </VStack>
            )}
          </VStack>
          {reveal && <ValueDialog reveal={reveal} onClose={hide} />}
          {adding && (
            <AddSecret
              spaces={spaceNames}
              spacesError={spaces.error}
              onAdded={() => {
                setAdding(false);
                list.reload();
              }}
              onCancel={() => setAdding(false)}
            />
          )}
          {changing?.action === "set" && (
            <SetValue key={changing.secret.id} secret={changing.secret} onDone={changed} onCancel={stopChanging} />
          )}
          {changing?.action === "rename" && (
            <RenameSecret key={changing.secret.id} secret={changing.secret} onDone={changed} onCancel={stopChanging} />
          )}
          {changing?.action === "move" && (
            <MoveSecret
              key={changing.secret.id}
              secret={changing.secret}
              spaces={spaceNames ?? []}
              spacesError={spaces.error}
              onDone={changed}
              onCancel={stopChanging}
            />
          )}
          {changing?.action === "remove" && (
            <ConfirmDialog
              title={t("secret.remove.title", { name: qualified(changing.secret) })}
              consequence={t("secret.remove.consequence", { name: qualified(changing.secret) })}
              confirm={t("secret.remove.confirm")}
              onCancel={stopChanging}
              onConfirm={() => {
                const { secret } = changing;
                setChanging(undefined);
                void remove.submit({ space: secret.space, name: secret.name }).then((removed) => {
                  if (removed) {
                    changed(secret);
                  }
                });
              }}
            />
          )}
        </LayoutContent>
      }
    />
  );
}

/** RowActions is a Secret's overflow menu: set its value, rename, move or delete it. */
function RowActions({ name, onChoose }: { name: string; onChoose: (action: Change["action"]) => void }) {
  const t = useT();
  return (
    <DropdownMenu
      alignment="end"
      presentation="adaptive"
      renderTrigger={(trigger) => (
        <IconButton
          {...trigger}
          variant="ghost"
          size="sm"
          label={t("secret.actions.label", { name })}
          tooltip={t("secret.actions.label", { name })}
          icon={<Icon icon={Ellipsis} size="sm" />}
        />
      )}
      items={[
        { label: t("secret.set.button"), onClick: () => onChoose("set") },
        { label: t("secret.rename.button"), onClick: () => onChoose("rename") },
        { label: t("secret.move.button"), onClick: () => onChoose("move") },
        { type: "divider" },
        { label: t("secret.remove.button"), variant: "destructive", onClick: () => onChoose("remove") },
      ]}
    />
  );
}

/**
 * ValueDialog shows the value of the Secret asked for, once secret.show
 * answered: as text, its lines kept, or as its base64 when its bytes are not
 * UTF-8, in a CopyLine, which a click selects whole and a button copies.
 * Closing the dialog forgets the value.
 */
function ValueDialog({ reveal, onClose }: { reveal: Reveal; onClose: () => void }) {
  const t = useT();
  const { secret, value, error } = reveal;
  return (
    <AdaptiveDialog
      title={t("secret.value.label", { name: qualified(secret) })}
      onClose={onClose}
      footer={
        <HStack hAlign="end">
          <Button variant="secondary" label={t("secret.value.hide")} onClick={onClose} />
        </HStack>
      }
    >
      {error !== undefined ? (
        <ErrorMessage error={error} />
      ) : value ? (
        <VStack gap={3}>
          {!value.utf8 && (
            <Text type="body" color="secondary">
              {t("secret.value.base64")}
            </Text>
          )}
          <CopyLine line={value.text} />
          <Text type="body" color="secondary">
            {t("secret.value.note")}
          </Text>
        </VStack>
      ) : (
        <Spinner label={t("secret.value.loading")} />
      )}
    </AdaptiveDialog>
  );
}

/** ChangeProps are what the dialog of a write to one Secret gets. */
interface ChangeProps {
  secret: Secret;
  /** onDone hears that the write succeeded. */
  onDone: (secret: Secret) => void;
  onCancel: () => void;
}

/** ValueField is the field a value is typed into: several lines, such as a private key, kept as typed. */
function ValueField({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <TextArea
      label={label}
      value={value}
      onChange={onChange}
      rows={5}
      autoComplete="off"
      hasSpellCheck={false}
      autoCapitalize="off"
      hasAutoCorrect={false}
      controlClassName="koko-secret-field"
    />
  );
}

/** SetValue replaces a Secret's value with one typed in; the last writer wins. */
function SetValue({ secret, onDone, onCancel }: ChangeProps) {
  const t = useT();
  const [value, setValue] = useState("");
  const set = useWrite(SecretSet);
  return (
    <FormDialog
      title={t("secret.set.title", { name: qualified(secret) })}
      submit={t("secret.set.submit")}
      ready={value !== "" && !set.pending}
      error={set.error}
      onSubmit={() =>
        void set
          .submit({ space: secret.space, name: secret.name, value_base64: encodeValue(value) })
          .then((written) => written && onDone(secret))
      }
      onClose={onCancel}
    >
      <ValueField label={t("secret.set.value")} value={value} onChange={setValue} />
    </FormDialog>
  );
}

/** RenameSecret gives a Secret a new name in its Space. */
function RenameSecret({ secret, onDone, onCancel }: ChangeProps) {
  const t = useT();
  const [newName, setNewName] = useState("");
  const rename = useWrite(SecretRename);
  return (
    <FormDialog
      title={t("secret.rename.title", { name: qualified(secret) })}
      submit={t("secret.rename.submit")}
      ready={newName !== "" && !rename.pending}
      error={rename.error}
      onSubmit={() =>
        void rename
          .submit({ space: secret.space, name: secret.name, new_name: newName })
          .then((written) => written && onDone(secret))
      }
      onClose={onCancel}
    >
      <TextInput
        label={t("secret.rename.name")}
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

/** MoveSecret takes a Secret, under its name, to another Space the caller can act in. */
function MoveSecret({
  secret,
  spaces,
  spacesError,
  onDone,
  onCancel,
}: ChangeProps & { spaces: string[]; spacesError: unknown }) {
  const t = useT();
  const [newSpace, setNewSpace] = useState("");
  const move = useWrite(SecretMove);
  return (
    <FormDialog
      title={t("secret.move.title", { name: qualified(secret) })}
      submit={t("secret.move.submit")}
      ready={newSpace !== "" && !move.pending}
      error={move.error ?? spacesError}
      onSubmit={() =>
        void move
          .submit({ space: secret.space, name: secret.name, new_space: newSpace })
          .then((written) => written && onDone(secret))
      }
      onClose={onCancel}
    >
      <Selector
        label={t("secret.move.space")}
        placeholder={t("secret.move.choose")}
        options={spaces.filter((space) => space !== secret.space)}
        value={newSpace}
        onChange={setNewSpace}
        presentation="adaptive"
      />
    </FormDialog>
  );
}

/**
 * AddSecret adds a Secret, its value typed in, to a Space the caller can
 * act in: their Personal Space unless they choose another. It never asks.
 */
function AddSecret({
  spaces,
  spacesError,
  onAdded,
  onCancel,
}: {
  spaces: string[] | undefined;
  spacesError: unknown;
  onAdded: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const caller = useCaller();
  const [space, setSpace] = useState(caller.user.name);
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const add = useWrite(SecretAdd);
  return (
    <FormDialog
      title={t("secret.add.title")}
      submit={t("secret.add.submit")}
      ready={name !== "" && value !== "" && !add.pending}
      error={add.error ?? spacesError}
      onSubmit={() =>
        void add.submit({ space, name, value_base64: encodeValue(value) }).then((added) => added && onAdded())
      }
      onClose={onCancel}
    >
      <Selector
        label={t("secret.add.space")}
        options={spaces ?? [space]}
        value={space}
        onChange={setSpace}
        presentation="adaptive"
      />
      <TextInput
        label={t("secret.add.name")}
        value={name}
        onChange={setName}
        autoComplete="off"
        autoCapitalize="off"
        hasAutoCorrect={false}
        hasSpellCheck={false}
      />
      <ValueField label={t("secret.add.value")} value={value} onChange={setValue} />
    </FormDialog>
  );
}
