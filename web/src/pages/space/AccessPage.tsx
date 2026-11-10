import { Button, Checkbox, Chip, Link, Switch } from "@heroui/react";
import { ChevronRight, Lock, Plus, UserPlus } from "lucide-react";
import { type ReactNode, useId, useRef, useState } from "react";

import { DeviceList, type DeviceSession } from "../../api/device";
import { type Space, SpaceAdd, type SpaceDetail, SpaceList, SpaceShow } from "../../api/space";
import { type User, UserList } from "../../api/user";
import { useCaller } from "../../shell/caller";
import { type Read, useRead, useReads, useWrite } from "../../shell/data";
import { type Count, type Translate, useCount, useLanguage, useT } from "../../shell/i18n";
import { accountPage, navigate, type PageProps, useLocation, useQueryFlag } from "../../shell/router";
import { Initial } from "../../shell/TopBar";
import { Field } from "../../ui/fields";
import { FormDialog } from "../../ui/FormDialog";
import { notify } from "../../ui/notify";
import { Page, Sheet } from "../../ui/Page";
import { usePhone } from "../../ui/phone";
import { LoadFailed, Loading, Refusal } from "../../ui/states";
import { Invite } from "../user/Invite";
import { PersonPanel } from "../user/PersonPanel";
import { type Membership, useMembership } from "./membership";
import { accessPath, personPath, spacePath } from "./path";
import { SpacePanel } from "./SpacePanel";

/**
 * AccessPage is Access (docs/web-design.md, "Access"): for the Owner, the
 * Users against the shared Spaces in one table; for a Member, the Spaces
 * they can act in. A Space's panel (/access/spaces/<name>) and, for the
 * Owner alone, a person's (/access/people/<name>) open over it, each a path
 * of its own, so the page keeps its state beneath them.
 */
export function AccessPage({ params }: PageProps) {
  const caller = useCaller();
  const { pathname, search } = useLocation();
  const name = params["name"];
  const panel: Panel | undefined =
    name === undefined ? undefined : { kind: pathname.startsWith(personPath("")) ? "person" : "space", name };
  // A panel closes back to Access as it was beneath it, its query kept, in
  // place of the panel's history entry: not a new visit, so the focus goes
  // back to the link that opened it rather than to the main region.
  const close = () => navigate(accessPath + search, { replace: true });
  return caller.owner ? <OwnerAccess panel={panel} close={close} /> : <MemberAccess panel={panel} close={close} />;
}

/** Panel is the panel open over Access: a person's or a Space's, by name. */
interface Panel {
  kind: "person" | "space";
  name: string;
}

/** SpaceShown is a Space, with its detail as space.show answered it. */
interface SpaceShown {
  space: Space;
  detail: SpaceDetail;
}

/** SpaceRead is the Spaces a caller sees, and each one's detail as space.show answers it. */
interface SpaceRead {
  spaces: Read<{ spaces: Space[] }>;
  details: Read<{ space: SpaceDetail }[]>;
  /**
   * shown are the Spaces read, each with its detail, once both are in; while
   * they are read again after a change, the last ones shown, so the page
   * keeps its rows, and the links an open panel gives the focus back to.
   */
  shown?: SpaceShown[];
}

/** useSpaces reads the Spaces the caller sees that keep pass, then space.show for each. */
function useSpaces(keep: (s: Space) => boolean): SpaceRead {
  const spaces = useRead(SpaceList, {});
  const kept = spaces.data?.spaces.filter(keep);
  const details = useReads(
    SpaceShow,
    (kept ?? []).map((s) => ({ name: s.name })),
  );
  const fresh =
    kept && details.data?.length === kept.length
      ? kept.flatMap((space, i) => {
          const detail = details.data?.[i]?.space;
          return detail ? [{ space, detail }] : [];
        })
      : undefined;
  const last = useRef(fresh);
  if (fresh) {
    last.current = fresh;
  }
  const failed = spaces.error !== undefined || details.error !== undefined;
  return { spaces, details, shown: fresh ?? (failed ? undefined : last.current) };
}

/** OwnerAccess is the Owner's Access: the table of Users against shared Spaces, Add Space and Invite. */
function OwnerAccess({ panel, close: closePanel }: { panel?: Panel; close: () => void }) {
  const t = useT();
  const phone = usePhone();
  const [all, setAll] = useQueryFlag("all");
  const users = useRead(UserList, all ? { all: true } : {});
  const devices = useRead(DeviceList, { all: true });
  const { spaces, details, shown } = useSpaces((s) => !s.personal);
  const membership = useMembership(details.data, details.reload);
  const [dialog, setDialog] = useState<"space" | "invite">();
  // A refusal a panel showed goes with it, rather than showing above the table after.
  const close = () => {
    membership.dismiss();
    closePanel();
  };
  const reload = () => {
    users.reload();
    devices.reload();
    spaces.reload();
    details.reload();
  };
  const reads = [users, devices, spaces, details];
  const failed = reads.find((r) => r.error !== undefined);
  const shared = shown?.map((s) => s.space);
  const ready =
    users.data && devices.data && shown
      ? { users: byRole(users.data.users), devices: devices.data.device_sessions, spaces: shown }
      : undefined;

  return (
    <Page
      title={t("shell.place.access")}
      lead={t("space.access.lead")}
      actions={
        <>
          <Button variant="secondary" onPress={() => setDialog("space")}>
            <Plus aria-hidden className="size-4" />
            {t("space.add")}
          </Button>
          <Button onPress={() => setDialog("invite")}>
            <UserPlus aria-hidden className="size-4" />
            {t("user.invite")}
          </Button>
        </>
      }
    >
      {failed !== undefined && (
        <Sheet list className="mb-6">
          <LoadFailed error={failed.error} retry={reload} />
        </Sheet>
      )}
      {ready ? (
        <>
          <Refusal error={panel === undefined ? membership.error : undefined} />
          {phone ? (
            <PhoneLists {...ready} />
          ) : (
            <AccessTable {...ready} membership={membership} />
          )}
        </>
      ) : (
        failed === undefined && (
          <Sheet list>
            <Loading />
          </Sheet>
        )
      )}
      <div className="mt-6">
        <Switch isSelected={all} onChange={setAll} size="sm">
          <Switch.Content>
            <Switch.Control>
              <Switch.Thumb />
            </Switch.Control>
            {t("user.showRemoved")}
          </Switch.Content>
        </Switch>
      </div>
      {panel?.kind === "person" && (
        <PersonPanel
          name={panel.name}
          shared={shared}
          membership={membership}
          onChanged={reload}
          onClose={close}
        />
      )}
      {panel?.kind === "space" && (
        <SpacePanel name={panel.name} membership={membership} onChanged={reload} onClose={close} />
      )}
      {dialog === "space" && (
        <AddSpace
          onAdded={() => {
            setDialog(undefined);
            spaces.reload();
          }}
          onCancel={() => setDialog(undefined)}
        />
      )}
      {dialog === "invite" && (
        <Invite shared={shared ?? []} onInvited={reload} onClose={() => setDialog(undefined)} />
      )}
    </Page>
  );
}

/** byRole orders the Users as Access lists them: the Owner first and the Removed Users last, each group as user.list answered it. */
function byRole(users: readonly User[]): User[] {
  const rank = (u: User) => (u.role === "owner" ? 0 : u.removed_at === undefined ? 1 : 2);
  return [...users].sort((a, b) => rank(a) - rank(b));
}

/** liveCount counts the Users who are not Removed, as the table's and the list's heading say. */
function liveCount(users: readonly User[]): number {
  return users.filter((u) => u.removed_at === undefined).length;
}

/**
 * waysIn says how a User gets in: their live devices and whether they set a
 * Password; "Invited, not yet in" for one who has done neither and never
 * had a device; "Removed" for a Removed User.
 */
function waysIn(t: Translate, count: Count, user: User, devices: readonly DeviceSession[]): string {
  if (user.removed_at !== undefined) {
    return t("user.removed");
  }
  const own = devices.filter((d) => d.user === user.name);
  const live = own.filter((d) => d.revoked_at === undefined).length;
  const parts = [
    ...(live > 0 ? [count("device.count", live)] : []),
    ...(user.password_set_at === undefined ? [] : [t("user.passwordSet")]),
  ];
  if (parts.length > 0) {
    return parts.join(" · ");
  }
  return t(own.length === 0 ? "user.notYetIn" : "user.noLiveDevice");
}

/** Person is a User's link to their panel: the initial, the name, "Owner" where so, and how they get in. */
function Person({ user, line }: { user: User; line: string }) {
  const t = useT();
  const { search } = useLocation();
  const removed = user.removed_at !== undefined;
  return (
    <Link
      href={personPath(user.name) + search}
      className="row-link -mx-2 flex w-[calc(100%+1rem)] items-center gap-3 px-2 py-2 text-foreground no-underline"
    >
      <Initial name={user.name} className="size-8 text-sm" />
      <span className="min-w-0">
        <span className="flex items-center gap-2">
          <span className={`t-name truncate ${removed ? "text-muted line-through" : ""}`}>{user.name}</span>
          {user.role === "owner" && <span className="t-label">{t("shell.role.owner")}</span>}
        </span>
        <span className="block truncate text-sm text-muted">{line}</span>
      </span>
    </Link>
  );
}

/** countsOf says what a Space holds: its Hosts and Secrets. */
function countsOf(t: Translate, count: Count, detail: SpaceDetail): string {
  return t("space.counts", {
    hosts: count("host.count.hosts", detail.counts.hosts),
    secrets: count("secret.count", detail.counts.secrets),
  });
}

/**
 * AccessTable is the Owner's table on a desktop: a row per User, a column
 * per shared Space, and in each cell a checkbox of the User's membership,
 * which changes at once. The Owner's cells show a lock, since the Owner sees
 * every Space, and a Removed User's none.
 */
function AccessTable({
  users,
  devices,
  spaces,
  membership,
}: {
  users: readonly User[];
  devices: readonly DeviceSession[];
  spaces: readonly SpaceShown[];
  membership: Membership;
}) {
  const t = useT();
  const count = useCount();
  const { search } = useLocation();
  const live = liveCount(users);
  return (
    <div className="sheet overflow-x-auto px-5 pt-4 pb-2">
      <table aria-label={t("space.access.table")} className="w-full border-separate border-spacing-0">
        <thead>
          <tr>
            <th
              scope="col"
              className="sticky left-0 z-10 w-72 min-w-64 bg-surface pr-4 pb-3 text-left align-bottom font-normal"
            >
              <span className="t-label">{count("space.access.users", live)}</span>
            </th>
            {spaces.map(({ space, detail }) => (
              <th key={space.id} scope="col" className="min-w-40 px-2 pb-3 text-center align-bottom font-normal">
                <Link
                  href={spacePath(space.name) + search}
                  className="row-link inline-flex flex-col items-center px-3 py-1.5 text-foreground no-underline"
                >
                  <span className="t-name">{space.name}</span>
                  <span className="t-num text-[0.8125rem] whitespace-nowrap text-muted">{countsOf(t, count, detail)}</span>
                </Link>
              </th>
            ))}
            <td className="w-full" />
          </tr>
        </thead>
        <tbody>
          {users.map((u) => (
            <tr key={u.id}>
              <th scope="row" className="sticky left-0 z-10 border-t border-line bg-surface py-2 pr-4 text-left font-normal">
                <Person user={u} line={waysIn(t, count, u, devices)} />
              </th>
              {spaces.map(({ space }) => (
                <td key={space.id} className="border-t border-line px-2 py-2 text-center">
                  <Cell user={u} space={space.name} membership={membership} />
                </td>
              ))}
              <td className="border-t border-line" />
            </tr>
          ))}
        </tbody>
      </table>
      {spaces.length === 0 ? (
        <p className="max-w-[60ch] border-t border-line py-4 text-muted">{t("space.access.noSharedLead")}</p>
      ) : (
        <p className="t-label flex items-center gap-2 border-t border-line py-3">
          <Lock aria-hidden className="size-4" />
          {t("space.access.ownerSeesAll")}
        </p>
      )}
    </div>
  );
}

/** Cell is one User's membership of one Space: a checkbox, the Owner's lock, or nothing for a Removed User. */
function Cell({ user, space, membership }: { user: User; space: string; membership: Membership }) {
  const t = useT();
  if (user.removed_at !== undefined) {
    return <span aria-hidden className="text-muted">–</span>;
  }
  if (user.role === "owner") {
    return (
      <span
        role="img"
        aria-label={t("space.access.ownerCell", { space })}
        className="inline-flex size-11 items-center justify-center text-muted"
      >
        <Lock aria-hidden className="size-4" />
      </span>
    );
  }
  return (
    <Checkbox
      aria-label={t("space.access.cell", { user: user.name, space })}
      isSelected={membership.isMember(space, user.name)}
      onChange={(on) => void membership.set(space, user.name, on)}
      className="inline-flex p-2.5"
    >
      <Checkbox.Content>
        <Checkbox.Control>
          <Checkbox.Indicator />
        </Checkbox.Control>
      </Checkbox.Content>
    </Checkbox>
  );
}

/**
 * PhoneLists is the Owner's table on a phone, read from either side: the
 * People, each opening their panel with the Spaces as switches, then the
 * Spaces, each opening its panel with its Members.
 */
function PhoneLists({
  users,
  devices,
  spaces,
}: {
  users: readonly User[];
  devices: readonly DeviceSession[];
  spaces: readonly SpaceShown[];
}) {
  const t = useT();
  const count = useCount();
  const live = liveCount(users);
  return (
    <div className="flex flex-col gap-6">
      <Group title={count("space.access.users", live)}>
        <ul className="sheet overflow-hidden px-3">
          {users.map((u) => (
            <li key={u.id} className="border-b border-line py-1.5 last:border-0">
              <Person user={u} line={waysIn(t, count, u, devices)} />
            </li>
          ))}
        </ul>
      </Group>
      <Group title={t("space.access.spaces")}>
        {spaces.length === 0 ? (
          <p className="text-muted">{t("space.access.noSharedLead")}</p>
        ) : (
          <SpaceRows spaces={spaces} />
        )}
      </Group>
    </div>
  );
}

/** Group is a heading over what it names, as a region. */
function Group({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id}>
      <h2 id={id} className="t-heading mb-2 px-1">
        {title}
      </h2>
      {children}
    </section>
  );
}

/** SpaceRows lists Spaces, each a link to its panel: its name, "Personal Space" where so, its counts and Members. */
function SpaceRows({ spaces }: { spaces: readonly SpaceShown[] }) {
  const t = useT();
  const count = useCount();
  const language = useLanguage();
  const { search } = useLocation();
  const list = new Intl.ListFormat(language, { type: "conjunction" });
  return (
    <ul className="sheet overflow-hidden px-3">
      {spaces.map(({ space, detail }) => (
        <li key={space.id} className="border-b border-line last:border-0">
          <Link
            href={spacePath(space.name) + search}
            className="row-link -mx-2 flex w-[calc(100%+1rem)] items-center justify-between gap-4 px-2 py-3.5 text-foreground no-underline"
          >
            <span className="min-w-0">
              <span className="flex items-center gap-2">
                <span className="t-name truncate">{space.name}</span>
                {space.personal && <Chip variant="secondary">{t("space.personal")}</Chip>}
              </span>
              <span className="block truncate text-sm text-muted">
                {countsOf(t, count, detail)}
                {!space.personal &&
                  ` · ${
                    detail.members.length === 0
                      ? t("space.members.noneShort")
                      : list.format(detail.members.map((m) => m.name))
                  }`}
              </span>
            </span>
            <ChevronRight aria-hidden className="size-5 shrink-0 text-muted" />
          </Link>
        </li>
      ))}
    </ul>
  );
}

/**
 * MemberAccess is a Member's Access: their Personal Space and the Spaces
 * they belong to, each opening its panel read-only, and where their own
 * devices are.
 */
function MemberAccess({ panel, close }: { panel?: Panel; close: () => void }) {
  const t = useT();
  const caller = useCaller();
  const { spaces, details, shown } = useSpaces(() => true);
  const failed = [spaces, details].find((r) => r.error !== undefined);
  const name = caller.user.name;
  return (
    <Page title={t("shell.place.access")} lead={t("space.access.memberLead", { name })}>
      {failed !== undefined && (
        <Sheet list className="mb-6">
          <LoadFailed
            error={failed.error}
            retry={() => {
              spaces.reload();
              details.reload();
            }}
          />
        </Sheet>
      )}
      {shown ? (
        <SpaceRows spaces={shown} />
      ) : (
        failed === undefined && (
          <Sheet list>
            <Loading />
          </Sheet>
        )
      )}
      <p className="t-label mt-6 max-w-[60ch]">
        <Link href={accountPage}>{t("space.access.memberNote", { name })}</Link>
      </p>
      {panel?.kind === "space" && <SpacePanel name={panel.name} onChanged={details.reload} onClose={close} />}
    </Page>
  );
}

/** AddSpace adds a shared Space: one field, its name. */
function AddSpace({ onAdded, onCancel }: { onAdded: () => void; onCancel: () => void }) {
  const t = useT();
  const [name, setName] = useState("");
  const add = useWrite(SpaceAdd);
  return (
    <FormDialog
      title={t("space.add")}
      lead={t("space.add.lead")}
      submit={t("space.add")}
      pending={add.pending}
      error={add.error}
      onSubmit={() =>
        void add.submit({ name }).then((added) => {
          if (added) {
            notify(t("space.added", { name: added.space.name }));
            onAdded();
          }
        })
      }
      onClose={onCancel}
    >
      <Field
        label={t("space.add.name")}
        value={name}
        onChange={setName}
        code
        autoFocus
        validate={(v) => (v === "" ? t("space.add.nameRequired") : undefined)}
      />
    </FormDialog>
  );
}
