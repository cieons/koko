import { useId, useState } from "react";

import {
  type SpaceDetail,
  SpaceMemberAdd,
  SpaceMemberRemove,
  SpaceRemove,
  SpaceRename,
  SpaceShow,
} from "../../api/space";
import { UserList } from "../../api/user";
import { OwnerOnly, useCaller } from "../../shell/caller";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { ErrorMessage, useT } from "../../shell/i18n";
import { navigate, type PageProps } from "../../shell/router";
import { spacePath } from "./SpaceListPage";

/**
 * SpaceDetailPage shows a Space the caller can act in: whether it is a
 * Personal Space, its Members, and how many records it holds. The Owner
 * renames and removes a shared Space here and changes its Members; a
 * Personal Space offers none of these, as the koko server refuses them while
 * its User exists.
 */
export function SpaceDetailPage({ params }: PageProps) {
  const t = useT();
  const show = useRead(SpaceShow, { name: params["name"] ?? "" });
  if (!show.data) {
    return show.error !== undefined ? <ErrorMessage error={show.error} /> : null;
  }
  const { space } = show.data;
  return (
    <>
      <h1>{space.name}</h1>
      <p>{space.personal ? t("space.detail.personal") : t("space.detail.shared")}</p>
      {show.error !== undefined && <ErrorMessage error={show.error} />}
      <Members space={space} onChanged={show.reload} />
      <h2>{t("space.detail.holds")}</h2>
      <dl>
        <dt>{t("space.counts.hosts")}</dt>
        <dd>{space.counts.hosts}</dd>
        <dt>{t("space.counts.retiredHosts")}</dt>
        <dd>{space.counts.retired_hosts}</dd>
        <dt>{t("space.counts.secrets")}</dt>
        <dd>{space.counts.secrets}</dd>
      </dl>
      {!space.personal && (
        <OwnerOnly>
          <RenameSpace name={space.name} />
          <RemoveSpace name={space.name} />
        </OwnerOnly>
      )}
    </>
  );
}

/**
 * Members lists a Space's Members. For a shared Space the Owner takes a
 * Member out and adds one; both are reversible writes and never ask.
 */
function Members({ space, onChanged }: { space: SpaceDetail; onChanged: () => void }) {
  const t = useT();
  const titleId = useId();
  const mayChange = useCaller().owner && !space.personal;
  const memberRemove = useWrite(SpaceMemberRemove);
  const takeOut = (user: string) =>
    void memberRemove.submit({ space: space.name, users: [user] }).then((done) => done && onChanged());
  return (
    <>
      <h2 id={titleId}>{t("space.detail.members")}</h2>
      {space.members.length === 0 ? (
        <p>{t("space.detail.noMembers")}</p>
      ) : (
        <ul aria-labelledby={titleId}>
          {space.members.map((member) => (
            <li key={member.id}>
              <span>{member.name}</span>
              {mayChange && (
                <>
                  {" "}
                  <button
                    type="button"
                    aria-label={t("space.member.remove.label", { user: member.name, space: space.name })}
                    onClick={() => takeOut(member.name)}
                  >
                    {t("space.member.remove")}
                  </button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      {memberRemove.error !== undefined && <ErrorMessage error={memberRemove.error} />}
      {mayChange && <AddMember space={space} onAdded={onChanged} />}
    </>
  );
}

/** AddMember is the Owner's form to make a User a Member, chosen among the Users who are not one. */
function AddMember({ space, onAdded }: { space: SpaceDetail; onAdded: () => void }) {
  const t = useT();
  const users = useRead(UserList, {});
  const [user, setUser] = useState("");
  const add = useWrite(SpaceMemberAdd);
  const choices = (users.data?.users ?? []).filter((u) => !space.members.some((m) => m.id === u.id));
  return (
    <form
      className="inline-form"
      onSubmit={(e) => {
        e.preventDefault();
        void add.submit({ space: space.name, users: [user] }).then((added) => {
          if (added) {
            setUser("");
            onAdded();
          }
        });
      }}
    >
      <label>
        {t("space.member.add.user")}{" "}
        <select value={user} onChange={(e) => setUser(e.target.value)}>
          <option value="">{t("space.member.add.choose")}</option>
          {choices.map((u) => (
            <option key={u.id} value={u.name}>
              {u.name}
            </option>
          ))}
        </select>
      </label>{" "}
      <button type="submit" disabled={user === "" || add.pending}>
        {t("space.member.add.submit")}
      </button>
      {users.error !== undefined && <ErrorMessage error={users.error} />}
      {add.error !== undefined && <ErrorMessage error={add.error} />}
    </form>
  );
}

/** RenameSpace is the Owner's form for a shared Space's new name; the page moves to it. */
function RenameSpace({ name }: { name: string }) {
  const t = useT();
  const [newName, setNewName] = useState("");
  const rename = useWrite(SpaceRename);
  return (
    <form
      className="inline-form"
      onSubmit={(e) => {
        e.preventDefault();
        void rename.submit({ name, new_name: newName }).then((renamed) => {
          if (renamed) {
            // The old name names no Space now, so it leaves no history entry.
            navigate(spacePath(renamed.space.name), { replace: true });
          }
        });
      }}
    >
      <label>
        {t("space.rename.name")}{" "}
        <input value={newName} onChange={(e) => setNewName(e.target.value)} autoComplete="off" />
      </label>{" "}
      <button type="submit" disabled={newName === "" || rename.pending}>
        {t("space.rename.submit")}
      </button>
      {rename.error !== undefined && <ErrorMessage error={rename.error} />}
    </form>
  );
}

/** RemoveSpace removes a shared Space once the Owner confirms, then goes back to the Spaces. */
function RemoveSpace({ name }: { name: string }) {
  const t = useT();
  const [asking, setAsking] = useState(false);
  const remove = useWrite(SpaceRemove);
  return (
    <div>
      <button type="button" onClick={() => setAsking(true)}>
        {t("space.remove.button")}
      </button>
      {remove.error !== undefined && <ErrorMessage error={remove.error} />}
      {asking && (
        <ConfirmDialog
          title={t("space.remove.title", { name })}
          consequence={t("space.remove.consequence", { name })}
          confirm={t("space.remove.confirm")}
          onCancel={() => setAsking(false)}
          onConfirm={() => {
            setAsking(false);
            void remove.submit({ name }).then((removed) => {
              if (removed) {
                navigate("/spaces", { replace: true });
              }
            });
          }}
        />
      )}
    </div>
  );
}
