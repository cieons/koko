import { Button, Link } from "@heroui/react";
import { ChevronRight } from "lucide-react";
import { useState } from "react";

import { DeviceList } from "../../api/device";
import { SignInList } from "../../api/signin";
import { CodeSpaceNotEmpty, type Space, SpaceShow } from "../../api/space";
import { type User, UserDemote, UserList, UserPromote, UserRemove } from "../../api/user";
import { ApiError } from "../../client";
import { browserName } from "../../shell/browserName";
import { mayActOn, roleName, useCaller } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { useCount, useT } from "../../shell/i18n";
import { useLocation } from "../../shell/router";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { SearchChoice } from "../../ui/fields";
import { useFocusFallback, useRowFocus } from "../../ui/focus";
import { Form } from "../../ui/Form";
import { notify } from "../../ui/notify";
import { SidePanel } from "../../ui/SidePanel";
import { AlertLine, Loaded, Loading, Refusal } from "../../ui/states";
import { Devices } from "../device/Devices";
import { PersonPassword } from "../signin/PersonPassword";
import { SignIns } from "../signin/SignIns";
import type { Membership } from "../space/membership";
import { spaceHostsPath, spacePath } from "../space/path";

/** PersonPanelProps are whose panel it is, and what it changes. */
export interface PersonPanelProps {
  name: string;
  /** shared are the shared Spaces, which the person is made a Member of or taken out of. */
  shared: readonly Space[] | undefined;
  membership: Membership;
  /** onChanged hears a change the panel made to the Users, so Access shows it. */
  onChanged: () => void;
  /** onClose closes the panel, as after Remove User. */
  onClose: () => void;
}

/**
 * PersonPanel is a person's panel over Access, the Owner's and the
 * Administrators' alone (docs/web-design.md, "Access"): their role and the
 * date they were added; the shared Spaces they are a Member of, each with
 * Take out, under Add to a Space, or, for the Owner and an Administrator,
 * that they see every Space; and their Personal Space's counts; their
 * devices (Devices), with Add device for them and Revoke; their Password,
 * with Issue Password Link and Remove Password, and their Browser Sign-ins
 * with Revoke; for the Owner, Make an Administrator, which asks first, or
 * Make a Member; and Remove User, which asks first and then says whether
 * the User was deleted or Removed. Where the caller may not act on the
 * person (mayActOn), as an Administrator on the Owner or another
 * Administrator, it offers none of those actions; on oneself, neither the
 * role's nor Remove User. A Removed User's panel says so and offers
 * nothing.
 * It finds the User in user.list with the Removed ones, so a bookmark of a
 * Removed User's panel opens too.
 */
export function PersonPanel({ name, shared, membership, onChanged, onClose }: PersonPanelProps) {
  const t = useT();
  // The Space picked in Add to a Space and not yet added, which a click beside the panel would lose.
  const [space, setSpace] = useState<string | null>(null);
  const users = useRead(UserList, { all: true });
  const user = users.data?.users.find((u) => u.name === name);
  const role = user && t(roleName(user));
  const since =
    user &&
    (user.removed_at === undefined
      ? t("user.added", { date: formatDate(user.created_at) })
      : t("user.removedOn", { date: formatDate(user.removed_at) }));
  return (
    <SidePanel title={name} subtitle={user && `${role} · ${since}`} changed={space !== null} onClose={onClose}>
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
              space={space}
              onSpace={setSpace}
              onPassword={() => {
                users.reload();
                onChanged();
              }}
              onRole={() => {
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
  space,
  onSpace,
  onPassword,
  onRole,
  onRemoved,
}: {
  user: User;
  shared: readonly Space[] | undefined;
  membership: Membership;
  /** space is the Space picked in Add to a Space, which the panel holds. */
  space: string | null;
  onSpace: (space: string | null) => void;
  /** onPassword hears the User's Password removed, so the panel and Access show it. */
  onPassword: () => void;
  /** onRole hears the User's role changed, so the panel and Access show it. */
  onRole: () => void;
  onRemoved: () => void;
}) {
  const t = useT();
  const caller = useCaller();
  const owner = user.role === "owner";
  const acts = mayActOn(caller, user);
  const self = user.name === caller.user.name;
  const [all, setAll] = useState(false);
  const [removing, setRemoving] = useState(false);
  // Removing the Password ends the User's Sign-ins, which are read again then.
  const [passwordChanges, setPasswordChanges] = useState(0);
  return (
    <>
      <section className="flex flex-col gap-3">
        <h3 className="t-heading">{t("user.spaces")}</h3>
        {user.role !== "member" ? (
          <p className="text-muted">{t("space.access.seesAll")}</p>
        ) : shared === undefined ? null : shared.length === 0 ? (
          <p className="text-muted">{t("space.access.noShared")}</p>
        ) : (
          <PersonSpaces user={user.name} shared={shared} membership={membership} space={space} onSpace={onSpace} />
        )}
        <PersonalSpace name={user.name} />
      </section>
      <Devices user={user.name} owner={owner} all={all} onAll={setAll} level={3} acts={acts} />
      <PersonPassword
        user={user}
        acts={acts}
        onChanged={() => {
          setPasswordChanges((n) => n + 1);
          onPassword();
        }}
      />
      <SignIns user={user.name} level={3} changes={passwordChanges} acts={acts} />
      {caller.owner && !owner && <Role user={user} onChanged={onRole} />}
      {acts && !owner && !self && (
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

/**
 * PersonSpaces are the shared Spaces a person is a Member of: how many of
 * the shared Spaces, a searchable Add to a Space among the others, and the
 * Spaces they are in, each a link to its panel with Take out beside it.
 * After Take out the focus stays in the list: the next Space's Take out, or
 * Add to a Space once none is left.
 */
function PersonSpaces({
  user,
  shared,
  membership,
  space,
  onSpace: setSpace,
}: {
  user: string;
  shared: readonly Space[];
  membership: Membership;
  space: string | null;
  onSpace: (space: string | null) => void;
}) {
  const t = useT();
  const count = useCount();
  const { search } = useLocation();
  // A Space is listed once the koko server answers that they are in it, and
  // a Space being taken out keeps its row, its Take out pending, until it
  // answers: isMember answers what a pending change asked for, so a pending
  // add is not listed yet and a pending Take out still is.
  const listed = (s: Space) => membership.isMember(s.name, user) !== membership.pending(s.name, user);
  const member = shared.filter(listed);
  const others = shared.filter((s) => !listed(s));
  const names = member.map((s) => s.name);
  const focus = useRowFocus<HTMLDivElement>(names.join("\n"));
  // Adding them to the last Space takes the form away: the line saying so then takes the focus.
  const [done, armDone] = useFocusFallback<HTMLParagraphElement>(others.length === 0);
  return (
    <>
      <p className="t-label t-num">
        {member.length === 0
          ? t("user.spaces.none")
          : count("user.spaces.count", shared.length, { member: member.length, total: shared.length })}
      </p>
      <div ref={focus.fallback}>
        {others.length === 0 ? (
          <p ref={done} tabIndex={-1} className="t-label outline-none">
            {t("user.spaces.every", { name: user })}
          </p>
        ) : (
          <Form
            label={t("user.spaces.add")}
            className="flex items-start gap-3"
            onSubmit={() => {
              if (space === null) {
                return;
              }
              // Adding them to the last Space takes the form away, as soon as the koko server answers.
              if (others.length === 1) {
                armDone();
              }
              void membership.set(space, user, true).then((ok) => ok && setSpace(null));
            }}
          >
            <SearchChoice
              label={t("user.spaces.add")}
              value={space}
              onChange={setSpace}
              options={others.map((s) => ({ id: s.name, label: s.name }))}
              validate={(v) => (v === null ? t("user.spaces.choose") : undefined)}
              className="flex-1"
            />
            <Button
              type="submit"
              variant="secondary"
              className="mt-6 shrink-0"
              isPending={space !== null && membership.pending(space, user)}
            >
              {t("space.member.addSubmit")}
            </Button>
          </Form>
        )}
      </div>
      <Refusal error={membership.error} />
      {member.length > 0 && (
        <ul className="flex flex-col">
          {member.map((s) => (
            <li key={s.id} className="flex items-center justify-between gap-3 border-t border-line py-2">
              <Link
                href={spacePath(s.name) + search}
                className="row-link -mx-2 flex min-w-0 flex-1 items-center gap-1 px-2 py-1.5 text-foreground no-underline"
              >
                <span className="t-name truncate">{s.name}</span>
                <ChevronRight aria-hidden className="size-4 shrink-0 text-muted" />
              </Link>
              <Button
                ref={focus.row(s.name)}
                variant="tertiary"
                aria-label={t("space.member.takeOutLabel", { user, space: s.name })}
                isPending={membership.pending(s.name, user)}
                onPress={() => {
                  focus.arm(names, s.name);
                  void membership.set(s.name, user, false).then((ok) => ok || focus.disarm());
                }}
              >
                {t("space.member.takeOut")}
              </Button>
            </li>
          ))}
        </ul>
      )}
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
 * Role is the Owner's change of a User's role, one button that keeps the
 * focus as the role changes: Make an Administrator for a Member, which asks
 * first (Promote), and Make a Member for an Administrator, which does not,
 * as Take out does not. A refusal shows in place.
 */
function Role({ user, onChanged }: { user: User; onChanged: () => void }) {
  const t = useT();
  const [promoting, setPromoting] = useState(false);
  const demote = useWrite(UserDemote);
  const { name } = user;
  const administrator = user.role === "administrator";
  const changed = (key: "user.promoted" | "user.demoted") => {
    notify(t(key, { name }));
    onChanged();
  };
  return (
    <section className="mt-2 flex flex-col items-start gap-3 border-t border-line pt-6">
      <p className="t-label max-w-[52ch]">{t(administrator ? "user.demote.lead" : "user.promote.lead", { name })}</p>
      <Button
        variant="secondary"
        isPending={demote.pending}
        onPress={() =>
          administrator
            ? void demote.submit({ name }).then((done) => done && changed("user.demoted"))
            : setPromoting(true)
        }
      >
        {t(administrator ? "user.demote" : "user.promote")}
      </Button>
      <Refusal error={demote.error} />
      {promoting && (
        <Promote
          name={name}
          onPromoted={() => {
            setPromoting(false);
            changed("user.promoted");
          }}
          onCancel={() => setPromoting(false)}
        />
      )}
    </section>
  );
}

/**
 * Promote makes a Member an Administrator, asking first: what it gives, and
 * the User's live devices, each with who added it, and their live Sign-ins,
 * which promotion keeps, for the Owner to read before acting (ADR 0056).
 * The action waits until both are read.
 */
function Promote({ name, onPromoted, onCancel }: { name: string; onPromoted: () => void; onCancel: () => void }) {
  const t = useT();
  const promote = useWrite(UserPromote);
  const devices = useRead(DeviceList, { user: name });
  const signins = useRead(SignInList, { user: name });
  const kept = devices.data && signins.data && { devices: devices.data.device_sessions, signins: signins.data.signins };
  return (
    <ConfirmDialog
      title={t("user.promote.title", { name })}
      consequence={t("user.promote.consequence", { name })}
      confirm={t("user.promote")}
      details={
        kept ? (
          <>
            <Kept
              title={t("user.promote.devices")}
              none={t("user.promote.noDevice")}
              items={kept.devices.map((d) => ({
                id: d.id,
                text: d.added_by
                  ? t("user.promote.device", { device: d.device, by: `${d.added_by.user}/${d.added_by.device}` })
                  : t("user.promote.deviceElsewhere", { device: d.device }),
              }))}
            />
            <Kept
              title={t("user.promote.signins")}
              none={t("user.promote.noSignin")}
              items={kept.signins.map((s) => ({
                id: s.id,
                text: t("user.promote.signin", { client: browserName(t, s), date: formatDate(s.started_at) }),
              }))}
            />
          </>
        ) : (
          devices.error === undefined && signins.error === undefined && <Loading />
        )
      }
      waiting={!kept}
      pending={promote.pending}
      error={promote.error ?? devices.error ?? signins.error}
      onCancel={onCancel}
      onConfirm={() => void promote.submit({ name }).then((promoted) => promoted && onPromoted())}
    />
  );
}

/** Kept is a short list, under its title, of what a promotion keeps, or a line saying there is none. */
function Kept({ title, none, items }: { title: string; none: string; items: readonly { id: string; text: string }[] }) {
  return (
    <section className="flex flex-col gap-1">
      <h3 className="t-label font-semibold text-foreground">{title}</h3>
      {items.length === 0 ? (
        <p className="text-sm text-muted">{none}</p>
      ) : (
        <ul className="flex flex-col gap-0.5 text-sm">
          {items.map((item) => (
            <li key={item.id}>{item.text}</li>
          ))}
        </ul>
      )}
    </section>
  );
}

/**
 * RemoveUser removes a User, asking first, and then says from the call's
 * outcome whether they were deleted, having never used koko, or Removed.
 * A refusal with space_not_empty names their Personal Space, which still
 * holds records and which the People list does not show.
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
