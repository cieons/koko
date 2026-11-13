import { Button, Link } from "@heroui/react";
import { useState } from "react";

import { type SpaceDetail, SpaceRemove, SpaceRename, SpaceShow } from "../../api/space";
import { UserList } from "../../api/user";
import { useCaller } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { Initial } from "../../shell/controls";
import { useCount, useT } from "../../shell/i18n";
import { navigate, useLocation } from "../../shell/router";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { Field, SearchChoices } from "../../ui/fields";
import { useFocusFallback, useRowFocus } from "../../ui/focus";
import { Form } from "../../ui/Form";
import { FormDialog } from "../../ui/FormDialog";
import { notify } from "../../ui/notify";
import { SidePanel } from "../../ui/SidePanel";
import { Loaded, Refusal } from "../../ui/states";
import type { Membership } from "./membership";
import { spaceHostsPath, spacePath, spaceSecretsPath } from "./path";

/** SpacePanelProps are which Space a panel shows, and what it changes. */
export interface SpacePanelProps {
  name: string;
  /** membership changes the Members, the Owner's alone. */
  membership?: Membership;
  /** onChanged hears a change the panel made, so Access shows it. */
  onChanged: () => void;
  /** onClose closes the panel, as after Remove Space. */
  onClose: () => void;
}

/**
 * SpacePanel is a Space's panel over Access (docs/web-design.md, "Access"):
 * its counts of Hosts, Retired Hosts and Secrets, each a link to its list
 * narrowed to the Space, and its Members (space.show). The Owner, on a
 * shared Space, takes a Member out, adds one, renames it, after which the
 * panel moves to the new name in place of its history entry, and removes
 * it, asking first. A Personal Space says it takes no Members and offers
 * none of these; a Member sees the panel read-only.
 */
export function SpacePanel({ name, membership, onChanged, onClose }: SpacePanelProps) {
  const t = useT();
  const caller = useCaller();
  const read = useRead(SpaceShow, { name });
  const detail = read.data?.space;
  const editable = caller.administers && detail !== undefined && !detail.personal;
  const [dialog, setDialog] = useState<"rename" | "remove">();
  // After Take out the focus stays in the Members: the next one's Take out, or Add Members once none is left.
  const focus = useRowFocus<HTMLDivElement>(read.data);
  // The Users picked in Add Members and not yet added, which a click beside the panel would lose.
  const [picked, setPicked] = useState<string[]>([]);
  const changed = () => {
    read.reload();
    onChanged();
  };
  return (
    <SidePanel
      title={name}
      subtitle={detail && (detail.personal ? t("space.personalOf", { name }) : t("space.shared"))}
      changed={picked.length > 0}
      onClose={onClose}
      footer={
        editable ? (
          <>
            <Button variant="danger-soft" className="mr-auto" onPress={() => setDialog("remove")}>
              {t("space.remove")}
            </Button>
            <Button variant="secondary" onPress={() => setDialog("rename")}>
              {t("space.rename")}
            </Button>
          </>
        ) : undefined
      }
    >
      <Loaded read={read}>
        {({ space }) => (
          <>
            <Counts space={space} />
            <section className="flex flex-col gap-3">
              <h3 className="t-heading">{t("space.members")}</h3>
              {space.personal ? (
                <p className="text-muted">{t("space.personalLead", { name: space.name })}</p>
              ) : (
                <>
                  {editable && membership && (
                    <div ref={focus.fallback}>
                      <AddMembers
                        space={space}
                        membership={membership}
                        users={picked}
                        onUsers={setPicked}
                        onAdded={read.reload}
                      />
                    </div>
                  )}
                  {editable && membership && <Refusal error={membership.error} />}
                  {space.members.length === 0 ? (
                    <p className="text-muted">{t("space.members.none")}</p>
                  ) : (
                    <ul className="flex flex-col">
                      {space.members.map((m) => (
                        <li key={m.id} className="flex items-center justify-between gap-3 border-t border-line py-3">
                          <span className="flex min-w-0 items-center gap-3">
                            <Initial name={m.name} className="size-8 text-sm" />
                            <span className="t-name truncate">{m.name}</span>
                          </span>
                          {editable && membership && (
                            <Button
                              ref={focus.row(m.name)}
                              variant="tertiary"
                              aria-label={t("space.member.takeOutLabel", { user: m.name, space: space.name })}
                              isPending={membership.pending(space.name, m.name)}
                              onPress={() => {
                                focus.arm(
                                  space.members.map((n) => n.name),
                                  m.name,
                                );
                                void membership.set(space.name, m.name, false).then((ok) => {
                                  if (ok) {
                                    read.reload();
                                  } else {
                                    focus.disarm();
                                  }
                                });
                              }}
                            >
                              {t("space.member.takeOut")}
                            </Button>
                          )}
                        </li>
                      ))}
                    </ul>
                  )}
                  <p className="t-label">{t("space.members.ownerNote")}</p>
                </>
              )}
            </section>
          </>
        )}
      </Loaded>
      {dialog === "rename" && (
        <RenameSpace
          name={name}
          onRenamed={() => {
            setDialog(undefined);
            onChanged();
          }}
          onCancel={() => setDialog(undefined)}
        />
      )}
      {dialog === "remove" && (
        <RemoveSpace
          name={name}
          onRemoved={() => {
            onChanged();
            onClose();
          }}
          onCancel={() => setDialog(undefined)}
          onRefused={changed}
        />
      )}
    </SidePanel>
  );
}

/** Counts are a Space's counts of Hosts, Retired Hosts and Secrets, each a link to its list narrowed to the Space. */
function Counts({ space }: { space: SpaceDetail }) {
  const t = useT();
  const counts = [
    { kind: t("space.counts.hosts"), count: space.counts.hosts, href: spaceHostsPath(space.name) },
    { kind: t("space.counts.retiredHosts"), count: space.counts.retired_hosts, href: spaceHostsPath(space.name, true) },
    { kind: t("space.counts.secrets"), count: space.counts.secrets, href: spaceSecretsPath(space.name) },
  ];
  return (
    <dl className="grid grid-cols-3 gap-4 rounded-xl border border-border bg-background p-5">
      {counts.map(({ kind, count, href }) => (
        <div key={kind} className="flex min-w-0 flex-col gap-0.5">
          <dt className="t-label">{kind}</dt>
          <dd>
            <Link href={href} className="t-figure" aria-label={t("space.counts.link", { kind, space: space.name, count })}>
              {count}
            </Link>
          </dd>
        </div>
      ))}
    </dl>
  );
}

/**
 * AddMembers makes several Users Members of the Space at once, in one
 * space.member.add, chosen in a searchable choice of several among those
 * who are not one (user.list): neither the Owner, who sees every Space, nor
 * a Removed User. The panel holds the Users picked (users), so a click
 * beside it does not lose them.
 */
function AddMembers({
  space,
  membership,
  users: picked,
  onUsers: setPicked,
  onAdded,
}: {
  space: SpaceDetail;
  membership: Membership;
  users: readonly string[];
  onUsers: (users: string[]) => void;
  onAdded: () => void;
}) {
  const t = useT();
  const count = useCount();
  const users = useRead(UserList, {});
  // Those just added leave the list as the koko server answers, before the panel reads the Space again.
  const candidates = (users.data?.users ?? []).filter(
    (u) =>
      u.role !== "owner" &&
      u.removed_at === undefined &&
      !space.members.some((m) => m.name === u.name) &&
      !membership.isMember(space.name, u.name),
  );
  const everyone = users.data !== undefined && candidates.length === 0;
  // Adding the last of them takes the form away: the line saying so then takes the focus.
  const [done, armDone] = useFocusFallback<HTMLParagraphElement>(everyone);
  if (users.error !== undefined) {
    return <Refusal error={users.error} />;
  }
  if (everyone) {
    return (
      <p ref={done} tabIndex={-1} className="t-label outline-none">
        {t("space.member.everyone")}
      </p>
    );
  }
  return (
    <Form
      label={t("space.member.add")}
      className="flex items-start gap-3"
      onSubmit={() => {
        // Adding every one left takes the form away, as soon as the koko server answers.
        if (picked.length === candidates.length) {
          armDone();
        }
        void membership.add(space.name, picked).then((ok) => {
          if (ok) {
            setPicked([]);
            onAdded();
          }
        });
      }}
    >
      <SearchChoices
        label={t("space.member.add")}
        values={picked}
        onChange={setPicked}
        options={candidates.map((u) => ({ id: u.name, label: u.name }))}
        placeholder={t("space.member.placeholder")}
        searchLabel={t("space.member.search")}
        noMatch={t("space.access.noPerson")}
        required={t("space.member.choose")}
        className="min-w-0 flex-1"
      />
      <Button type="submit" variant="secondary" className="mt-6 shrink-0" isPending={membership.adding(space.name)}>
        {picked.length === 0 ? t("space.member.addSubmit") : count("space.member.addMany", picked.length)}
      </Button>
    </Form>
  );
}

/** RenameSpace gives a shared Space a new name, then moves the panel to it in place of its history entry. */
function RenameSpace({ name, onRenamed, onCancel }: { name: string; onRenamed: () => void; onCancel: () => void }) {
  const t = useT();
  const { search } = useLocation();
  const [newName, setNewName] = useState(name);
  const rename = useWrite(SpaceRename);
  return (
    <FormDialog
      title={t("space.rename.title", { name })}
      submit={t("space.rename")}
      pending={rename.pending}
      changed={newName !== name}
      error={rename.error}
      onSubmit={() =>
        void rename.submit({ name, new_name: newName }).then((renamed) => {
          if (renamed) {
            notify(t("space.renamed", { name, newName: renamed.space.name }));
            onRenamed();
            navigate(spacePath(renamed.space.name) + search, { replace: true });
          }
        })
      }
      onClose={onCancel}
    >
      <Field
        label={t("space.rename.name")}
        value={newName}
        onChange={setNewName}
        code
        autoFocus
        validate={(v) => (v === "" ? t("space.rename.nameRequired") : undefined)}
      />
    </FormDialog>
  );
}

/** RemoveSpace removes a shared Space with its memberships, asking first; space_not_empty shows in place. */
function RemoveSpace({
  name,
  onRemoved,
  onRefused,
  onCancel,
}: {
  name: string;
  onRemoved: () => void;
  onRefused: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const remove = useWrite(SpaceRemove);
  return (
    <ConfirmDialog
      title={t("space.remove.title", { name })}
      consequence={t("space.remove.consequence", { name })}
      confirm={t("space.remove.confirm")}
      pending={remove.pending}
      error={remove.error}
      onCancel={onCancel}
      onConfirm={() =>
        void remove.submit({ name }).then((removed) => {
          if (removed) {
            notify(t("space.removed", { name }));
            onRemoved();
          } else {
            onRefused();
          }
        })
      }
    />
  );
}
