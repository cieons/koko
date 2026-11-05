import { useId, useState } from "react";

import { DeviceAdd, DeviceList, DeviceRevoke, type DeviceSession, type JoinCode } from "../../api/device";
import { UserList } from "../../api/user";
import { type Caller, useCaller } from "../../shell/caller";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, type Translate, useT } from "../../shell/i18n";
import { useQueryFlag } from "../../shell/router";
import { JoinCodeLine, useShownOnce } from "./JoinCodeLine";

/**
 * DevicesPage lists the caller's own Device Sessions, or everyone's for the
 * Owner, the Revoked ones too when asked (?all=1, as `koko device ls --all`).
 * A live one is revoked once the caller confirms, and the Owner's own last
 * one only once its name is typed: the web UI's form of --last. The page
 * also adds a device: issuing its Join Code never asks, and the code shows
 * once, until the page is left.
 */
export function DevicesPage() {
  const t = useT();
  const caller = useCaller();
  const titleId = useId();
  const [all, showRevoked] = useQueryFlag("all");
  const list = useRead(DeviceList, all ? { all: true } : {});
  const revoke = useWrite(DeviceRevoke);
  const [revoking, setRevoking] = useState<DeviceSession>();
  const sessions = list.data?.device_sessions ?? [];
  const ownersLive = sessions.filter((s) => caller.owner && s.user === caller.user.name && !s.revoked_at);
  const ownersLast = (s: DeviceSession) => ownersLive.length === 1 && ownersLive[0]?.id === s.id;

  return (
    <>
      <h1 id={titleId}>{t("device.list.title")}</h1>
      <label>
        <input type="checkbox" checked={all} onChange={(e) => showRevoked(e.target.checked)} />{" "}
        {t("device.list.showRevoked")}
      </label>
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {revoke.error !== undefined && <ErrorMessage error={revoke.error} />}
      {list.data && (
        <table aria-labelledby={titleId}>
          <thead>
            <tr>
              {caller.owner && <th>{t("device.column.user")}</th>}
              <th>{t("device.column.device")}</th>
              <th>{t("device.column.addedBy")}</th>
              <th>{t("device.column.joined")}</th>
              <th>{t("device.column.lastUsed")}</th>
              {all && <th>{t("device.column.revoked")}</th>}
              <th />
            </tr>
          </thead>
          <tbody>
            {sessions.map((session) => (
              <tr key={session.id}>
                {caller.owner && <td>{session.user}</td>}
                <td>
                  {session.device}
                  {session.id === caller.deviceSession?.id && <span className="tag"> {t("device.here")}</span>}
                </td>
                <td>
                  {session.added_by
                    ? `${session.added_by.user}/${session.added_by.device}`
                    : t("device.addedBy.socket")}
                </td>
                <td>{formatDate(session.created_at)}</td>
                <td>{session.last_used_at ? formatDate(session.last_used_at) : t("device.lastUsed.never")}</td>
                {all && <td>{session.revoked_at ? formatDate(session.revoked_at) : ""}</td>}
                <td>
                  {!session.revoked_at && (
                    <button
                      type="button"
                      aria-label={t("device.revoke.label", { name: shownName(session, caller) })}
                      onClick={() => setRevoking(session)}
                    >
                      {t("device.revoke.button")}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {revoking && (
        <ConfirmDialog
          title={t("device.revoke.title", { name: shownName(revoking, caller) })}
          consequence={revokeConsequence(t, revoking, caller, ownersLast(revoking))}
          confirm={t("device.revoke.confirm")}
          typedName={ownersLast(revoking) ? revoking.device : undefined}
          onCancel={() => setRevoking(undefined)}
          onConfirm={() => {
            setRevoking(undefined);
            const last = ownersLast(revoking) || undefined;
            void revoke.submit({ user: revoking.user, device: revoking.device, last }).then(list.reload);
          }}
        />
      )}
      <AddDevice />
    </>
  );
}

/**
 * shownName names a Device Session as the CLI does: bare for one of the
 * caller's own devices, and <user>/<device> for another User's.
 */
function shownName(session: DeviceSession, caller: Caller): string {
  return session.user === caller.user.name ? session.device : `${session.user}/${session.device}`;
}

/**
 * revokeConsequence states what revoking session does: for the Owner's last
 * device, that only the koko server's own machine adds another; for the
 * device this page runs through, that the page stops; for one that is both,
 * both; else that the device is cut off.
 */
function revokeConsequence(t: Translate, session: DeviceSession, caller: Caller, ownersLast: boolean): string {
  const name = shownName(session, caller);
  const here = session.id === caller.deviceSession?.id;
  if (ownersLast) {
    return here ? t("device.revoke.consequenceLastHere", { name }) : t("device.revoke.consequenceLast", { name });
  }
  return here ? t("device.revoke.consequenceHere", { name }) : t("device.revoke.consequence", { name });
}

/**
 * AddDevice issues a Join Code for a new device: the caller's own, or, for
 * the Owner, any User's who is not Removed. It never asks.
 */
function AddDevice() {
  const t = useT();
  const caller = useCaller();
  const [device, setDevice] = useState("");
  const [user, setUser] = useState(caller.user.name);
  const [joinCode, setJoinCode] = useShownOnce<JoinCode>();
  const add = useWrite(DeviceAdd);
  return (
    <>
      <h2>{t("device.add.title")}</h2>
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault();
          const request = { device: device || undefined, user: caller.owner ? user : undefined };
          void add.submit(request).then((added) => {
            if (added) {
              setDevice("");
              setJoinCode(added.join_code);
            }
          });
        }}
      >
        {caller.owner && <UserChoice user={user} onChange={setUser} />}
        <label>
          {t("device.add.name")}{" "}
          <input value={device} onChange={(e) => setDevice(e.target.value)} autoComplete="off" />
        </label>{" "}
        <button type="submit" disabled={add.pending}>
          {t("device.add.submit")}
        </button>
        {add.error !== undefined && <ErrorMessage error={add.error} />}
      </form>
      {joinCode && <JoinCodeLine key={joinCode.id} joinCode={joinCode} />}
    </>
  );
}

/** UserChoice is the Owner's choice of whose device to add, among the Users who are not Removed. */
function UserChoice({ user, onChange }: { user: string; onChange: (user: string) => void }) {
  const t = useT();
  const users = useRead(UserList, {});
  const names = users.data?.users.map((u) => u.name) ?? [user];
  return (
    <>
      <label>
        {t("device.add.user")}{" "}
        <select value={user} onChange={(e) => onChange(e.target.value)}>
          {names.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
      </label>{" "}
      {users.error !== undefined && <ErrorMessage error={users.error} />}
    </>
  );
}
