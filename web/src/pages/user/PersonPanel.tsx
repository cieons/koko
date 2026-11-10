import { Button, Link, Switch } from "@heroui/react";
import { useState } from "react";

import { CodeSpaceNotEmpty, type Space, SpaceShow } from "../../api/space";
import { type User, UserList, UserRemove } from "../../api/user";
import { ApiError } from "../../client";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { useCount, useT } from "../../shell/i18n";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { notify } from "../../ui/notify";
import { SidePanel } from "../../ui/SidePanel";
import { AlertLine, Loaded, Refusal } from "../../ui/states";
import { Devices } from "../device/Devices";
import { PersonPassword } from "../signin/PersonPassword";
import { SignIns } from "../signin/SignIns";
import type { Membership } from "../space/membership";
import { spaceHostsPath } from "../space/path";

/** PersonPanelProps are whose panel it is, and what it changes. */
export interface PersonPanelProps {
  name: string;
  /** shared are the shared Spaces, each a switch of the person's membership. */
  shared: readonly Space[] | undefined;
  membership: Membership;
  /** onChanged hears a change the panel made to the Users, so Access shows it. */
  onChanged: () => void;
  /** onClose closes the panel, as after Remove User. */
  onClose: () => void;
}

/**
 * PersonPanel is a person's panel over Access, the Owner's alone
 * (docs/web-design.md, "Access"): their role and the date they were added;
 * the shared Spaces as switches of their membership, and their Personal
 * Space's counts; their devices (Devices), with Add device for them and
 * Revoke; their Password, with Issue Password Link and Remove Password, and
 * their Browser Sign-ins with Revoke; and Remove User, which asks first and
 * then says whether the User was deleted or Removed. A Removed User's panel
 * says so and offers nothing.
 * It finds the User in user.list with the Removed ones, so a bookmark of a
 * Removed User's panel opens too.
 */
export function PersonPanel({ name, shared, membership, onChanged, onClose }: PersonPanelProps) {
  const t = useT();
  const users = useRead(UserList, { all: true });
  const user = users.data?.users.find((u) => u.name === name);
  const role = user && t(user.role === "owner" ? "shell.role.owner" : "shell.role.member");
  const since =
    user &&
    (user.removed_at === undefined
      ? t("user.added", { date: formatDate(user.created_at) })
      : t("user.removedOn", { date: formatDate(user.removed_at) }));
  return (
    <SidePanel title={name} subtitle={user && `${role} · ${since}`} onClose={onClose}>
      <Loaded read={users}>
        {() =>
          user === undefined ? (
            <AlertLine>{t("error.unknown_user")}</AlertLine>
          ) : user.removed_at !== undefined ? (
            <p className="text-muted">{t("user.removedLead")}</p>
          ) : (
            <Person
              user={user}
              shared={shared}
              membership={membership}
              onPassword={() => {
                users.reload();
                onChanged();
              }}
              onRemoved={() => {
                onChanged();
                onClose();
              }}
            />
          )
        }
      </Loaded>
    </SidePanel>
  );
}

/** Person is what a panel shows of a User who is not Removed. */
function Person({
  user,
  shared,
  membership,
  onPassword,
  onRemoved,
}: {
  user: User;
  shared: readonly Space[] | undefined;
  membership: Membership;
  /** onPassword hears the User's Password removed, so the panel and Access show it. */
  onPassword: () => void;
  onRemoved: () => void;
}) {
  const t = useT();
  const owner = user.role === "owner";
  const [all, setAll] = useState(false);
  const [removing, setRemoving] = useState(false);
  // Removing the Password ends the User's Sign-ins, which are read again then.
  const [passwordChanges, setPasswordChanges] = useState(0);
  return (
    <>
      <section className="flex flex-col gap-3">
        <h3 className="t-heading">{t("user.spaces")}</h3>
        {owner ? (
          <p className="text-muted">{t("space.access.ownerSeesAll")}</p>
        ) : shared === undefined ? null : shared.length === 0 ? (
          <p className="text-muted">{t("space.access.noShared")}</p>
        ) : (
          <ul className="flex flex-col">
            {shared.map((s) => (
              <li key={s.id} className="border-t border-line py-3">
                <Switch
                  aria-label={t("space.access.cell", { user: user.name, space: s.name })}
                  isSelected={membership.isMember(s.name, user.name)}
                  onChange={(on) => void membership.set(s.name, user.name, on)}
                  className="w-full"
                >
                  <Switch.Content className="t-name w-full justify-between">
                    {s.name}
                    <Switch.Control>
                      <Switch.Thumb />
                    </Switch.Control>
                  </Switch.Content>
                </Switch>
              </li>
            ))}
          </ul>
        )}
        <Refusal error={membership.error} />
        <PersonalSpace name={user.name} />
      </section>
      <Devices user={user.name} owner={owner} all={all} onAll={setAll} level={3} />
      <PersonPassword
        user={user}
        onChanged={() => {
          setPasswordChanges((n) => n + 1);
          onPassword();
        }}
      />
      <SignIns user={user.name} level={3} changes={passwordChanges} />
      {!owner && (
        <section className="mt-2 flex flex-col items-start gap-3 border-t border-line pt-6">
          <p className="t-label max-w-[52ch]">{t("user.remove.lead")}</p>
          <Button variant="danger-soft" onPress={() => setRemoving(true)}>
            {t("user.remove.button", { name: user.name })}
          </Button>
        </section>
      )}
      {removing && <RemoveUser name={user.name} onRemoved={onRemoved} onCancel={() => setRemoving(false)} />}
    </>
  );
}

/** PersonalSpace says what a User's Personal Space holds, with a link to its Hosts. */
export function PersonalSpace({ name }: { name: string }) {
  const t = useT();
  const count = useCount();
  const read = useRead(SpaceShow, { name });
  const counts = read.data?.space.counts;
  if (read.error !== undefined) {
    return <Refusal error={read.error} />;
  }
  return (
    counts && (
      <p className="t-label">
        {t("space.personalLine", {
          name,
          hosts: count("host.count.hosts", counts.hosts),
          secrets: count("secret.count", counts.secrets),
        })}{" "}
        <Link href={spaceHostsPath(name)}>{t("space.viewHosts")}</Link>
      </p>
    )
  );
}

/**
 * RemoveUser removes a User, asking first, and then says from the call's
 * outcome whether they were deleted, having never used koko, or Removed.
 * A refusal with space_not_empty names their Personal Space, which still
 * holds records and which the Access table does not show.
 */
function RemoveUser({ name, onRemoved, onCancel }: { name: string; onRemoved: () => void; onCancel: () => void }) {
  const t = useT();
  const remove = useWrite(UserRemove);
  const error =
    remove.error instanceof ApiError && remove.error.code === CodeSpaceNotEmpty
      ? t("user.remove.spaceNotEmpty", { name })
      : remove.error;
  return (
    <ConfirmDialog
      title={t("user.remove.title", { name })}
      consequence={t("user.remove.consequence", { name })}
      confirm={t("user.remove.confirm")}
      pending={remove.pending}
      error={error}
      onCancel={onCancel}
      onConfirm={() =>
        void remove.submit({ name }).then((removed) => {
          if (removed) {
            notify(t(removed.outcome === "deleted" ? "user.deleted" : "user.removedDone", { name }));
            onRemoved();
          }
        })
      }
    />
  );
}
