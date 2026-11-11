import { useEffect, useRef, useState } from "react";

import { type SpaceDetail, SpaceMemberAdd, SpaceMemberRemove } from "../../api/space";
import { call } from "../../client";
import { useCount, useT } from "../../shell/i18n";
import { notify } from "../../ui/notify";

/** Membership is who is a Member of which shared Space, as Access shows it, and the change of one. */
export interface Membership {
  /** isMember tells whether user is a Member of space: as the koko server last answered, or as asked while the call runs. */
  isMember: (space: string, user: string) => boolean;
  /** pending tells whether a change of user's membership of space runs. */
  pending: (space: string, user: string) => boolean;
  /**
   * set makes user a Member of space, or takes them out, at once, with a
   * notice; it answers whether the koko server did. A second change of the
   * same membership while one runs is not sent.
   */
  set: (space: string, user: string, on: boolean) => Promise<boolean>;
  /**
   * add makes several Users Members of space in one space.member.add, with
   * one notice; it answers whether the koko server did. A second add to the
   * same Space while one runs is not sent.
   */
  add: (space: string, users: readonly string[]) => Promise<boolean>;
  /** adding tells whether an add of several Users to space runs. */
  adding: (space: string) => boolean;
  /** error is why the last change was refused, if it was. */
  error?: unknown;
  /** dismiss forgets error, as when the panel that showed it closes. */
  dismiss: () => void;
}

/**
 * useMembership is the membership of the Spaces given, as space.list
 * answered them, and its change: space.member.add or space.member.remove
 * for one User, or space.member.add for several, whose answer, the Space's
 * Members after it, shows until the Spaces are read again. onChanged hears each change made, to read again
 * what shows it.
 */
export function useMembership(
  spaces: readonly SpaceDetail[] | undefined,
  onChanged: () => void,
): Membership {
  const t = useT();
  const count = useCount();
  // changed holds each Space as a change answered it, until the Spaces are read again.
  const [changed, setChanged] = useState<Readonly<Record<string, SpaceDetail>>>({});
  // asked holds each change that runs, by space and user, and what it asked
  // for; an add of several Users, by space alone.
  const [asked, setAsked] = useState<Readonly<Record<string, boolean>>>({});
  // running holds the same keys at once, so a second change sent before a render is refused too.
  const running = useRef(new Set<string>());
  const [error, setError] = useState<unknown>();
  useEffect(() => setChanged({}), [spaces]);

  const key = (space: string, user: string) => JSON.stringify([space, user]);
  const isMember = (space: string, user: string) => {
    const wanted = asked[key(space, user)];
    if (wanted !== undefined) {
      return wanted;
    }
    const detail = changed[space] ?? spaces?.find((s) => s.name === space);
    return detail?.members.some((m) => m.name === user) === true;
  };
  const set = async (space: string, user: string, on: boolean) => {
    const k = key(space, user);
    if (running.current.has(k)) {
      return false;
    }
    running.current.add(k);
    setAsked((a) => ({ ...a, [k]: on }));
    setError(undefined);
    try {
      const request = { space, users: [user] };
      const answer = await (on ? call(SpaceMemberAdd, request) : call(SpaceMemberRemove, request));
      setChanged((c) => ({ ...c, [space]: answer.space }));
      notify(t(on ? "space.member.added" : "space.member.removed", { user, space }));
      onChanged();
      return true;
    } catch (failure) {
      setError(failure);
      return false;
    } finally {
      running.current.delete(k);
      setAsked(({ [k]: _, ...rest }) => rest);
    }
  };
  const add = async (space: string, users: readonly string[]) => {
    const k = JSON.stringify([space]);
    if (users.length === 0 || running.current.has(k)) {
      return false;
    }
    running.current.add(k);
    setAsked((a) => ({ ...a, [k]: true }));
    setError(undefined);
    try {
      const answer = await call(SpaceMemberAdd, { space, users: [...users] });
      setChanged((c) => ({ ...c, [space]: answer.space }));
      const [only] = users;
      notify(
        users.length === 1 && only !== undefined
          ? t("space.member.added", { user: only, space })
          : count("space.member.addedMany", users.length, { space }),
      );
      onChanged();
      return true;
    } catch (failure) {
      setError(failure);
      return false;
    } finally {
      running.current.delete(k);
      setAsked(({ [k]: _, ...rest }) => rest);
    }
  };
  return {
    add,
    adding: (space) => JSON.stringify([space]) in asked,
    isMember,
    pending: (space, user) => key(space, user) in asked,
    set,
    error,
    dismiss: () => setError(undefined),
  };
}
