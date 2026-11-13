import { Button, Chip, Link, SearchField, Switch, Tabs } from "@heroui/react";
import { ChevronRight, Plus, UserPlus } from "lucide-react";
import { type ReactNode, useState } from "react";

import { DeviceList, type DeviceSession } from "../../api/device";
import { SpaceAdd, type SpaceDetail, SpaceList } from "../../api/space";
import { type User, UserList } from "../../api/user";
import { roleName, useCaller } from "../../shell/caller";
import { Initial } from "../../shell/controls";
import { useRead, useWrite } from "../../shell/data";
import { type Count, type Translate, useCount, useT } from "../../shell/i18n";
import { type RememberedList, ResetView, useRememberedView } from "../../shell/rememberedView";
import { navigate, type PageProps, useLocation } from "../../shell/router";
import { Field, listSearchClass } from "../../ui/fields";
import { FormDialog } from "../../ui/FormDialog";
import { notify } from "../../ui/notify";
import { Page, Sheet } from "../../ui/Page";
import { LoadFailed, Loading, NoMatch } from "../../ui/states";
import { Invite } from "../user/Invite";
import { PersonPanel } from "../user/PersonPanel";
import { type Membership, useMembership } from "./membership";
import { accessPath, personPath, spacePath } from "./path";
import { SpacePanel } from "./SpacePanel";

/**
 * AccessPage is Access (docs/web-design.md, "Access"): for the Owner and an
 * Administrator, the People and the shared Spaces as two lists, one at a
 * time, each searched by name; for a Member, the Spaces they can act in. A
 * Space's panel (/access/spaces/<name>) and, for the Owner and an
 * Administrator alone, a person's
 * (/access/people/<name>) open over it, each a path of its own, so the page
 * keeps its state beneath them; membership is changed in either panel.
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
  return caller.administers ? <OwnerAccess panel={panel} close={close} /> : <MemberAccess panel={panel} close={close} />;
}

/** Panel is the panel open over Access: a person's or a Space's, by name. */
interface Panel {
  kind: "person" | "space";
  name: string;
}

/** View is which of the two lists the Owner's Access shows. */
type View = "people" | "spaces";

/**
 * accessList is the list Access's view is of: one, whose default view, the
 * People with no search, has no query. The view (view=spaces), the search
 * (q) and Show Removed Users (all=1) are remembered together.
 */
function accessList(): RememberedList {
  return { list: "access", base: "" };
}

/**
 * OwnerAccess is the Owner's Access, which an Administrator gets too: the
 * People and the shared Spaces as two lists, Add Space and Invite.
 */
function OwnerAccess({ panel, close: closePanel }: { panel?: Panel; close: () => void }) {
  const t = useT();
  const remembered = useRememberedView(accessList);
  const { params, setParams } = remembered;
  const view: View = params.get("view") === "spaces" ? "spaces" : "people";
  const q = params.get("q") ?? "";
  const all = params.get("all") === "1";
  // Each keystroke, tab and switch refines the view in place, the search as
  // it is typed, a tab or the switch as a visit of its own.
  const refine = (changes: Readonly<Partial<Record<"view" | "q" | "all", string>>>, replace = false) => {
    const next = new URLSearchParams(params);
    for (const [key, value] of Object.entries(changes)) {
      if (value === "") {
        next.delete(key);
      } else {
        next.set(key, value);
      }
    }
    setParams(next, { replace });
  };
  const users = useRead(UserList, all ? { all: true } : {});
  const devices = useRead(DeviceList, { all: true });
  const spaces = useRead(SpaceList, {});
  // useRead keeps the last answer while the Spaces are read again after a
  // change, so the page keeps its rows, and the links an open panel gives the
  // focus back to.
  const shared = spaces.data?.spaces.filter((s) => !s.personal);
  const membership = useMembership(spaces.data?.spaces, spaces.reload);
  const [dialog, setDialog] = useState<"space" | "invite">();
  // A refusal a panel showed goes with it.
  const close = () => {
    membership.dismiss();
    closePanel();
  };
  const reload = () => {
    users.reload();
    devices.reload();
    spaces.reload();
  };
  const failed = [users, devices, spaces].find((r) => r.error !== undefined);
  const ready =
    users.data && devices.data && shared
      ? { users: byRole(users.data.users), devices: devices.data.device_sessions, spaces: shared, membership }
      : undefined;

  return (
    <Page
      title={t("shell.place.access")}
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
        <Lists
          listed={ready}
          view={view}
          onView={(next) => refine({ view: next === "spaces" ? "spaces" : "" })}
          q={q}
          onSearch={(text) => refine({ q: text }, true)}
          reset={<ResetView view={remembered} />}
        />
      ) : (
        failed === undefined && (
          <Sheet list>
            <Loading />
          </Sheet>
        )
      )}
      {view === "people" && (
        <div className="mt-6">
          <Switch isSelected={all} onChange={(on) => refine({ all: on ? "1" : "" })} size="sm">
            <Switch.Content>
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
              {t("user.showRemoved")}
            </Switch.Content>
          </Switch>
        </div>
      )}
      {panel?.kind === "person" && (
        <PersonPanel name={panel.name} shared={shared} membership={membership} onChanged={reload} onClose={close} />
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
      {dialog === "invite" && <Invite shared={shared ?? []} onInvited={reload} onClose={() => setDialog(undefined)} />}
    </Page>
  );
}

/** Listed is what the Owner's lists are drawn from. */
interface Listed {
  /** users are the Users, the Owner, then the Administrators, then the Members, then the Removed ones, when shown. */
  users: readonly User[];
  devices: readonly DeviceSession[];
  /** spaces are the shared Spaces. */
  spaces: readonly SpaceDetail[];
  membership: Membership;
}

/**
 * byRole orders the Users as Access lists them: the Owner, then the
 * Administrators, then the Members, then the Removed Users, each group as
 * user.list answered it.
 */
function byRole(users: readonly User[]): User[] {
  const rank = (u: User) =>
    u.removed_at !== undefined ? 3 : u.role === "owner" ? 0 : u.role === "administrator" ? 1 : 2;
  return [...users].sort((a, b) => rank(a) - rank(b));
}

/** matches tells whether name holds q, whatever its case; an empty q matches every name. */
function matches(name: string, q: string): boolean {
  return name.toLocaleLowerCase().includes(q.trim().toLocaleLowerCase());
}

/**
 * Lists is the Owner's row of controls, the People and Spaces tabs with
 * their counts, the one search field and Reset view, over the list of the
 * tab chosen, narrowed by the search.
 */
function Lists({
  listed,
  view,
  onView,
  q,
  onSearch,
  reset,
}: {
  listed: Listed;
  view: View;
  onView: (view: View) => void;
  q: string;
  onSearch: (q: string) => void;
  reset: ReactNode;
}) {
  const t = useT();
  const { users, spaces } = listed;
  const people = users.filter((u) => matches(u.name, q));
  const found = spaces.filter((s) => matches(s.name, q));
  const clear = () => onSearch("");
  return (
    <Tabs selectedKey={view} onSelectionChange={(k) => onView(k === "spaces" ? "spaces" : "people")} className="gap-0">
      <div className="mb-5 flex flex-wrap items-center gap-2.5">
        {/* The tabs keep their width in the row, which wraps instead. */}
        <Tabs.ListContainer className="shrink-0">
          <Tabs.List aria-label={t("space.access.views")}>
            <Tabs.Tab id="people" className="w-auto whitespace-nowrap">
              {t("space.access.people")}
              <span className="t-num ml-1.5 text-muted">{users.filter((u) => u.removed_at === undefined).length}</span>
              <Tabs.Indicator />
            </Tabs.Tab>
            <Tabs.Tab id="spaces" className="w-auto whitespace-nowrap">
              {t("space.access.spaces")}
              <span className="t-num ml-1.5 text-muted">{spaces.length}</span>
              <Tabs.Indicator />
            </Tabs.Tab>
          </Tabs.List>
        </Tabs.ListContainer>
        <SearchField
          aria-label={t(view === "people" ? "space.access.search.people" : "space.access.search.spaces")}
          value={q}
          onChange={onSearch}
          className={listSearchClass}
          fullWidth
        >
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder={t("space.access.search.hint")} autoComplete="off" spellCheck="false" />
            <SearchField.ClearButton aria-label={t("space.access.search.clear")} />
          </SearchField.Group>
        </SearchField>
        {reset}
      </div>
      <Tabs.Panel id="people" className="p-0">
        {people.length === 0 ? (
          <NoMatch message={t("space.access.noPerson")} clear={t("space.access.search.clear")} onClear={clear} />
        ) : (
          <PeopleRows people={people} listed={listed} />
        )}
      </Tabs.Panel>
      <Tabs.Panel id="spaces" className="p-0">
        {spaces.length === 0 ? (
          <Sheet>
            <p className="text-muted">{t("space.access.noShared")}</p>
          </Sheet>
        ) : found.length === 0 ? (
          <NoMatch message={t("space.access.noSpace")} clear={t("space.access.search.clear")} onClear={clear} />
        ) : (
          <SpaceRows spaces={found} />
        )}
      </Tabs.Panel>
    </Tabs>
  );
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

/**
 * personLine is a person's one-line summary: the shared Spaces they are a
 * Member of, "Every Space" for the Owner and an Administrator, then how they get in; a Removed
 * User's is "Removed" alone.
 */
function personLine(t: Translate, count: Count, user: User, { devices, spaces, membership }: Listed): string {
  const ways = waysIn(t, count, user, devices);
  if (user.removed_at !== undefined) {
    return ways;
  }
  if (user.role !== "member") {
    return `${t("space.access.everySpace")} · ${ways}`;
  }
  const member = spaces.filter((s) => membership.isMember(s.name, user.name)).length;
  return `${member === 0 ? t("space.access.inNoSpace") : count("space.access.inSpaces", member)} · ${ways}`;
}

/** spaceLine is a Space's one-line summary: its Members, Hosts and Secrets; a Personal Space's has no Members. */
function spaceLine(t: Translate, count: Count, space: SpaceDetail): string {
  const hosts = count("host.count.hosts", space.counts.hosts);
  const secrets = count("secret.count", space.counts.secrets);
  if (space.personal) {
    return t("space.counts", { hosts, secrets });
  }
  const members = space.members.length;
  return t("space.access.spaceLine", {
    members: members === 0 ? t("space.members.noneShort") : count("space.members.count", members),
    hosts,
    secrets,
  });
}

const rowClass = "row-link -mx-2 grid w-[calc(100%+1rem)] items-center gap-x-3 px-2 py-2 text-foreground no-underline";

/**
 * listClass is the panel a list of rows lies on, a container its rows key
 * on: they lay their summary beside the name from 42rem of the list's own
 * width, which the rail beside the page narrows, and under it below that.
 */
const listClass = "sheet @container overflow-hidden px-4 md:px-5";

/** personRow lays a person's row out: initial, name, summary and chevron, the summary under the name on a narrow list. */
const personRow = `${rowClass} grid-cols-[auto_minmax(0,1fr)_auto] @2xl:grid-cols-[auto_minmax(0,18rem)_minmax(0,1fr)_auto]`;

/**
 * spaceRow lays a Space's row out as a person's, without the initial, its
 * summary in line with theirs: 20.75rem is a person's 18rem name column,
 * their 2rem initial and the 0.75rem gap between.
 */
const spaceRow = `${rowClass} grid-cols-[minmax(0,1fr)_auto] @2xl:grid-cols-[minmax(0,20.75rem)_minmax(0,1fr)_auto]`;

/**
 * PeopleRows is the People as a list on one panel: a row per person, a link
 * to their panel, with the initial, the name, "Owner" or "Administrator" where so, and the
 * one-line summary, beside the name on a wide list and under it on a narrow one.
 */
function PeopleRows({ people, listed }: { people: readonly User[]; listed: Listed }) {
  const t = useT();
  const count = useCount();
  const { search } = useLocation();
  return (
    <ul className={listClass}>
      {people.map((u) => (
        <li key={u.id} className="border-b border-line py-0.5 last:border-0">
          <Link href={personPath(u.name) + search} className={personRow}>
            <Initial name={u.name} className="row-span-2 size-8 text-sm @2xl:row-span-1" />
            <span className="flex min-w-0 items-center gap-2">
              <span className={`t-name truncate ${u.removed_at === undefined ? "" : "text-muted line-through"}`}>
                {u.name}
              </span>
              {u.role !== "member" && <span className="t-label">{t(roleName(u))}</span>}
            </span>
            <span className="col-start-2 row-start-2 truncate text-sm text-muted @2xl:col-start-3 @2xl:row-start-1">
              {personLine(t, count, u, listed)}
            </span>
            <ChevronRight
              aria-hidden
              className="col-start-3 row-span-2 row-start-1 size-5 text-muted @2xl:col-start-4 @2xl:row-span-1"
            />
          </Link>
        </li>
      ))}
    </ul>
  );
}

/**
 * SpaceRows is Spaces as a list on one panel: a row per Space, a link to its
 * panel, with its name, "Personal Space" where so, and its one-line summary,
 * beside the name on a wide list and under it on a narrow one.
 */
function SpaceRows({ spaces }: { spaces: readonly SpaceDetail[] }) {
  const t = useT();
  const count = useCount();
  const { search } = useLocation();
  return (
    <ul className={listClass}>
      {spaces.map((space) => (
        <li key={space.id} className="border-b border-line py-0.5 last:border-0">
          <Link href={spacePath(space.name) + search} className={spaceRow}>
            <span className="flex min-w-0 items-center gap-2 py-1.5 @2xl:py-1">
              <span className="t-name truncate">{space.name}</span>
              {space.personal && <Chip variant="secondary">{t("space.personal")}</Chip>}
            </span>
            <span className="t-num col-start-1 row-start-2 truncate text-sm text-muted @2xl:col-start-2 @2xl:row-start-1">
              {spaceLine(t, count, space)}
            </span>
            <ChevronRight
              aria-hidden
              className="col-start-2 row-span-2 row-start-1 size-5 text-muted @2xl:col-start-3 @2xl:row-span-1"
            />
          </Link>
        </li>
      ))}
    </ul>
  );
}

/**
 * MemberAccess is a Member's Access: their Personal Space and the Spaces
 * they belong to, each opening its panel read-only.
 */
function MemberAccess({ panel, close }: { panel?: Panel; close: () => void }) {
  const t = useT();
  const spaces = useRead(SpaceList, {});
  return (
    <Page title={t("shell.place.access")}>
      {spaces.error !== undefined && (
        <Sheet list className="mb-6">
          <LoadFailed error={spaces.error} retry={spaces.reload} />
        </Sheet>
      )}
      {spaces.data ? (
        <SpaceRows spaces={spaces.data.spaces} />
      ) : (
        spaces.error === undefined && (
          <Sheet list>
            <Loading />
          </Sheet>
        )
      )}
      {panel?.kind === "space" && <SpacePanel name={panel.name} onChanged={spaces.reload} onClose={close} />}
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
      submit={t("space.add")}
      pending={add.pending}
      changed={name !== ""}
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
