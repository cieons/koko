import { useId, useState } from "react";

import { CodeSpaceNotEmpty } from "../../api/space";
import { type User, UserInvite, type UserInviteResponse, UserList, UserRemove, type UserRemoveResponse } from "../../api/user";
import { ApiError } from "../../client";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, type Translate, useT } from "../../shell/i18n";
import { useQueryFlag } from "../../shell/router";
import { useShownOnce } from "../../shell/shownOnce";
import { JoinCodeLine } from "../device/JoinCodeLine";

/**
 * PeoplePage, the Owner's alone, lists the Users, the Removed ones too when
 * asked (?all=1, as `koko user ls --all`). The Owner invites a User here,
 * whose Invite shows once, and removes one once they confirm; the page then
 * says whether the User was deleted or Removed, from the answer.
 */
export function PeoplePage() {
  const t = useT();
  const titleId = useId();
  const [all, showRemoved] = useQueryFlag("all");
  const list = useRead(UserList, all ? { all: true } : {});
  const remove = useWrite(UserRemove);
  const [asking, setAsking] = useState<User>();
  // removing is the User the last removal was for, and removed its answer.
  const [removing, setRemoving] = useState<string>();
  const [removed, setRemoved] = useState<UserRemoveResponse>();

  return (
    <>
      <h1 id={titleId}>{t("user.list.title")}</h1>
      <label>
        <input type="checkbox" checked={all} onChange={(e) => showRemoved(e.target.checked)} />{" "}
        {t("user.list.showRemoved")}
      </label>
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {removed && <p role="status">{removalMessage(t, removed)}</p>}
      {remove.error !== undefined && removing !== undefined && <RemovalRefused error={remove.error} name={removing} />}
      {list.data && (
        <table aria-labelledby={titleId}>
          <thead>
            <tr>
              <th>{t("user.column.name")}</th>
              <th>{t("user.column.role")}</th>
              <th>{t("user.column.created")}</th>
              {all && <th>{t("user.column.removed")}</th>}
              <th />
            </tr>
          </thead>
          <tbody>
            {list.data.users.map((user) => (
              <tr key={user.id}>
                <td>{user.name}</td>
                <td>{roleName(t, user.role)}</td>
                <td>{formatDate(user.created_at)}</td>
                {all && <td>{user.removed_at ? formatDate(user.removed_at) : ""}</td>}
                <td>
                  {user.role !== "owner" && !user.removed_at && (
                    <button
                      type="button"
                      aria-label={t("user.remove.label", { name: user.name })}
                      onClick={() => setAsking(user)}
                    >
                      {t("user.remove.button")}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {asking && (
        <ConfirmDialog
          title={t("user.remove.title", { name: asking.name })}
          consequence={t("user.remove.consequence", { name: asking.name })}
          confirm={t("user.remove.confirm")}
          onCancel={() => setAsking(undefined)}
          onConfirm={() => {
            setAsking(undefined);
            setRemoving(asking.name);
            setRemoved(undefined);
            void remove.submit({ name: asking.name }).then((answer) => {
              setRemoved(answer);
              list.reload();
            });
          }}
        />
      )}
      <InviteUser onInvited={list.reload} />
    </>
  );
}

/** roleName is a User's role for people: Owner or Member, or the role as stored if neither. */
function roleName(t: Translate, role: string): string {
  return role === "owner" ? t("user.role.owner") : role === "member" ? t("user.role.member") : role;
}

/** removalMessage says what user.remove did, from its outcome. */
function removalMessage(t: Translate, { outcome, user }: UserRemoveResponse): string {
  if (outcome === "deleted") {
    return t("user.removed.deleted", { name: user.name });
  }
  if (outcome === "removed") {
    return t("user.removed.removed", { name: user.name });
  }
  return t("shell.error.unexpected"); // an outcome this web UI does not know says nothing it can trust
}

/**
 * RemovalRefused says why removing the User name was refused. A User who
 * never joined is deleted with their Personal Space, which must be empty, so
 * space_not_empty names that Space here, where the page shows none.
 */
function RemovalRefused({ error, name }: { error: unknown; name: string }) {
  const t = useT();
  if (error instanceof ApiError && error.code === CodeSpaceNotEmpty) {
    return (
      <p className="error" role="alert">
        {t("user.remove.spaceNotEmpty", { name })}
      </p>
    );
  }
  return <ErrorMessage error={error} />;
}

/**
 * InviteUser is the Owner's form for a new User, a Member at once, whose
 * Invite, their first Join Code, then shows once. It never asks.
 */
function InviteUser({ onInvited }: { onInvited: () => void }) {
  const t = useT();
  const [name, setName] = useState("");
  const [invited, setInvited] = useShownOnce<UserInviteResponse>();
  const invite = useWrite(UserInvite);
  return (
    <>
      <h2>{t("user.invite.title")}</h2>
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault();
          void invite.submit({ name }).then((answer) => {
            if (answer) {
              setName("");
              setInvited(answer);
              onInvited();
            }
          });
        }}
      >
        <label>
          {t("user.invite.name")}{" "}
          <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" />
        </label>{" "}
        <button type="submit" disabled={name === "" || invite.pending}>
          {t("user.invite.submit")}
        </button>
        {invite.error !== undefined && <ErrorMessage error={invite.error} />}
      </form>
      {invited && (
        <>
          <p>{t("user.invite.done", { name: invited.user.name })}</p>
          <JoinCodeLine key={invited.join_code.id} joinCode={invited.join_code} />
        </>
      )}
    </>
  );
}
