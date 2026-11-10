import { DeviceAdd, DeviceList, DeviceRevoke, type DeviceSession } from "../../api/device";
import { ServerShow } from "../../api/server";
import { PasswordLinkIssue, PasswordRemove, SignInList } from "../../api/signin";
import {
  SpaceAdd,
  type SpaceDetail,
  SpaceList,
  SpaceMemberAdd,
  SpaceMemberRemove,
  SpaceRemove,
  SpaceRename,
  SpaceShow,
} from "../../api/space";
import { type User, UserInvite, UserList, UserRemove } from "../../api/user";
import { type FakeApi, refusal } from "../../test/fakeApi";

// A koko server's Users, Spaces and Device Sessions for the tests of Access
// and Account: the fake API answers from them as the koko server would, and
// each write changes them, so a page's reload shows what it changed.

export const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
export const bob: User = {
  id: "user_2",
  name: "bob",
  role: "member",
  created_at: "2026-11-02T09:00:00Z",
  password_set_at: "2026-11-04T09:00:00Z",
};
export const carol: User = { id: "user_3", name: "carol", role: "member", created_at: "2026-11-05T09:00:00Z" };
export const dave: User = {
  id: "user_4",
  name: "dave",
  role: "member",
  created_at: "2026-11-01T10:00:00Z",
  removed_at: "2026-11-06T09:00:00Z",
};

/** space is a Space's detail: personal for a User's, else shared with members. */
export function space(
  name: string,
  { personal = false, members = [] as string[], hosts = 0, retired = 0, secrets = 0 } = {},
): SpaceDetail {
  return {
    id: `space_${name}`,
    name,
    personal,
    members: members.map((m) => ({ id: `user_${m}`, name: m })),
    counts: { hosts, retired_hosts: retired, secrets },
  };
}

/** device is a Device Session of user. */
export function device(user: string, name: string, more: Partial<DeviceSession> = {}): DeviceSession {
  return { id: `device_${user}_${name}`, user, device: name, created_at: "2026-11-03T09:00:00Z", ...more };
}

/** World is what the fake koko server holds. */
export interface World {
  users: User[];
  spaces: SpaceDetail[];
  devices: DeviceSession[];
  /** webURL is the Web URL, or null while it is not set. */
  webURL: string | null;
}

/**
 * world is a koko server with alice the Owner, bob a Member of ops, carol
 * invited and not yet in, and dave Removed; the shared Spaces ops and web,
 * and the Personal Spaces of the Users not Removed; alice's laptop and bob's laptop and
 * phone.
 */
export function world(): World {
  return {
    users: [alice, bob, carol, dave],
    spaces: [
      space("alice", { personal: true, hosts: 2, secrets: 1 }),
      space("bob", { personal: true, hosts: 1 }),
      space("carol", { personal: true }),
      space("ops", { members: ["bob"], hosts: 3, retired: 1, secrets: 3 }),
      space("web", { hosts: 1, secrets: 2 }),
    ],
    devices: [
      device("alice", "laptop", { last_used_at: "2026-11-07T09:00:00Z" }),
      device("bob", "laptop"),
      device("bob", "phone"),
      device("bob", "old", { revoked_at: "2026-11-04T09:00:00Z" }),
    ],
    webURL: "https://koko.example.com",
  };
}

/** passwordLink is the Password Link the fake issues for user. */
export function passwordLink(user: string): string {
  return `https://koko.example.com/password#LINKSECRETFOR${user.toUpperCase()}`;
}

/** joinCode is the Join Code the fake issues. */
export const joinCode = "koko_JOINCODE";

/**
 * serve makes the fake API answer from w as the koko server answers caller:
 * the Owner sees every Space and Device Session, a Member their own. Each
 * write changes w.
 */
export function serve(api: FakeApi, w: World, caller: User = alice): void {
  const owner = caller.role === "owner";
  const sees = (s: SpaceDetail) => owner || s.name === caller.name || s.members.some((m) => m.name === caller.name);
  const find = (name: string) => {
    const found = w.spaces.find((s) => s.name === name && sees(s));
    if (!found) {
      throw refusal("unknown_space", 404);
    }
    return found;
  };
  const replace = (next: SpaceDetail) => {
    w.spaces = w.spaces.map((s) => (s.id === next.id ? next : s));
    return { space: next };
  };
  // No browser is signed in, unless a test serves Sign-ins (signins.fixture.ts).
  api.on(SignInList, () => ({ signins: [] }));
  api.on(UserList, ({ all }) => ({ users: w.users.filter((u) => all === true || u.removed_at === undefined) }));
  api.on(SpaceList, () => ({
    spaces: w.spaces.filter(sees).map(({ id, name, personal }) => ({ id, name, personal })),
  }));
  api.on(SpaceShow, ({ name }) => ({ space: find(name) }));
  api.on(SpaceAdd, ({ name }) => {
    const added = space(name);
    w.spaces = [...w.spaces, added];
    return { space: { id: added.id, name, personal: false } };
  });
  api.on(SpaceRename, ({ name, new_name }) => {
    const renamed = replace({ ...find(name), name: new_name }).space;
    return { space: { id: renamed.id, name: new_name, personal: false } };
  });
  api.on(SpaceRemove, ({ name }) => {
    const removed = find(name);
    const { hosts, retired_hosts, secrets } = removed.counts;
    if (hosts + retired_hosts + secrets > 0) {
      throw refusal("space_not_empty", 409);
    }
    w.spaces = w.spaces.filter((s) => s.id !== removed.id);
    return { space: { id: removed.id, name, personal: false } };
  });
  api.on(SpaceMemberAdd, ({ space: name, users }) => {
    const s = find(name);
    const added = users.filter((u) => !s.members.some((m) => m.name === u)).map((u) => ({ id: `user_${u}`, name: u }));
    return replace({ ...s, members: [...s.members, ...added] });
  });
  api.on(SpaceMemberRemove, ({ space: name, users }) => {
    const s = find(name);
    return replace({ ...s, members: s.members.filter((m) => !users.includes(m.name)) });
  });
  api.on(UserInvite, ({ name, web }) => {
    if (web === true && w.webURL === null) {
      throw refusal("web_url_unset", 409);
    }
    const user: User = { id: `user_${name}`, name, role: "member", created_at: "2026-11-08T09:00:00Z" };
    w.users = [...w.users, user];
    w.spaces = [...w.spaces, space(name, { personal: true })];
    return web === true
      ? { user, password_link: { id: "link_1", user: name, link: passwordLink(name), expires_at: "2026-11-09T09:00:00Z" } }
      : {
          user,
          join_code: { id: "code_1", code: joinCode, user: name, device: "device-1", expires_at: "2026-11-08T09:15:00Z" },
        };
  });
  api.on(ServerShow, () => ({
    server: {
      address: "koko.example.com",
      listen_port: 7822,
      host_key_fingerprint: "SHA256:fingerprint",
      host_key: "ssh-ed25519 AAAA",
      version: "v0.3.0",
      root_key_file: "/etc/koko/root.key",
      root_key_id: "rootkey_1",
      data_dir: "/var/lib/koko",
      web_url: w.webURL,
      web_listen: w.webURL === null ? null : "127.0.0.1:7823",
    },
  }));
  api.on(PasswordLinkIssue, ({ user: name }) => {
    if (w.webURL === null) {
      throw refusal("web_url_unset", 409);
    }
    return { password_link: { id: "link_2", user: name, link: passwordLink(name), expires_at: "2026-11-09T09:00:00Z" } };
  });
  api.on(PasswordRemove, ({ user: name }) => {
    const user = w.users.find((u) => u.name === name);
    if (user?.password_set_at === undefined) {
      throw refusal("no_password", 409);
    }
    w.users = w.users.map((u) => (u.name === name ? { ...u, password_set_at: undefined } : u));
    return { user: name };
  });
  api.on(UserRemove, ({ name }) => {
    const user = w.users.find((u) => u.name === name);
    if (!user) {
      throw refusal("unknown_user", 404);
    }
    const used = w.devices.some((d) => d.user === name) || user.password_set_at !== undefined;
    if (!used) {
      w.users = w.users.filter((u) => u.name !== name);
      return { outcome: "deleted", user };
    }
    const removed = { ...user, removed_at: "2026-11-08T09:00:00Z" };
    w.users = w.users.map((u) => (u.name === name ? removed : u));
    return { outcome: "removed", user: removed };
  });
  api.on(DeviceList, ({ user, all }) => ({
    device_sessions: w.devices.filter(
      (d) =>
        (user === undefined ? owner || d.user === caller.name : d.user === user) &&
        (all === true || d.revoked_at === undefined),
    ),
  }));
  api.on(DeviceAdd, ({ user, device: name }) => ({
    join_code: {
      id: "code_2",
      code: joinCode,
      user: user ?? caller.name,
      device: name ?? "device-1",
      expires_at: "2026-11-08T09:15:00Z",
    },
  }));
  api.on(DeviceRevoke, ({ user, device: name, last }) => {
    const of = user ?? caller.name;
    const session = w.devices.find((d) => d.user === of && d.device === name && d.revoked_at === undefined);
    if (!session) {
      throw refusal("unknown_device", 404);
    }
    const ownerLive = w.devices.filter((d) => d.user === alice.name && d.revoked_at === undefined);
    if (of === alice.name && ownerLive.length === 1 && last !== true) {
      throw refusal("last_device_session", 409);
    }
    const revoked = { ...session, revoked_at: "2026-11-08T09:00:00Z" };
    w.devices = w.devices.map((d) => (d.id === session.id ? revoked : d));
    return { device_session: revoked };
  });
}
