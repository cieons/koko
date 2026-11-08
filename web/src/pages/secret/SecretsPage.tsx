import "./secret.css";

import { type ReactNode, useEffect, useId, useRef, useState } from "react";

import {
  type Secret,
  SecretAdd,
  SecretList,
  SecretMove,
  SecretRemove,
  SecretRename,
  SecretSet,
  SecretShow,
} from "../../api/secret";
import { type Space, SpaceList } from "../../api/space";
import { call } from "../../client";
import { useCaller } from "../../shell/caller";
import { ConfirmDialog } from "../../shell/ConfirmDialog";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, useT } from "../../shell/i18n";
import { useShownOnce } from "../../shell/shownOnce";
import { encodeValue, type ShownValue, showValue } from "./value";

/** revealFor is how long a revealed value stays on screen: 30 seconds (P0 spec: Secrets). */
const revealFor = 30_000;

/** Revealed is the one value on screen, and the Secret it is the value of. */
interface Revealed {
  id: string;
  value: ShownValue;
}

/** Change is a reversible write a person is filling in for one Secret. */
interface Change {
  secret: Secret;
  action: "set" | "rename" | "move";
}

/** qualified names a Secret as every command does: <space>/<secret>. */
function qualified(secret: Secret): string {
  return `${secret.space}/${secret.name}`;
}

/**
 * SecretsPage lists the names of the Secrets in every Space the caller can
 * act in, as `koko secret ls` does, and never a value until one is clicked:
 * secret.show reveals it, with a copy button, until 30 seconds pass, the
 * page is left or another value is revealed. A value lives only in the
 * page's state, never in the URL or the browser's storage, and the web
 * front sends it with Cache-Control: no-store. The page adds, sets,
 * renames, moves and deletes Secrets, a value typed into a form; only
 * deleting asks first.
 */
export function SecretsPage() {
  const t = useT();
  const titleId = useId();
  const list = useRead(SecretList, {});
  const spaces = useRead(SpaceList, {});
  const [revealed, setRevealed] = useShownOnce<Revealed>();
  const [revealError, setRevealError] = useState<unknown>();
  const [changing, setChanging] = useState<Change>();
  const [deleting, setDeleting] = useState<Secret>();
  const remove = useWrite(SecretRemove);
  const secrets = list.data?.secrets ?? [];
  const spaceNames = spaces.data?.spaces.map((s: Space) => s.name);
  // asking is the latest reveal, a new object for each reveal and each
  // hide, with the id of the Secret asked for. A value secret.show answers
  // after a later reveal, a hide, a write to its Secret or the browser
  // leaving the page is not asking's, and never shows: it may be out of
  // date, or no longer wanted.
  const asking = useRef<{ id?: string }>({});

  useEffect(() => {
    if (!revealed) {
      return;
    }
    const hide = setTimeout(() => setRevealed(undefined), revealFor);
    return () => clearTimeout(hide);
  }, [revealed, setRevealed]);

  useEffect(() => {
    const left = () => {
      asking.current = {};
    };
    window.addEventListener("pagehide", left);
    return () => window.removeEventListener("pagehide", left);
  }, []);

  const hide = () => {
    asking.current = {};
    setRevealed(undefined);
  };
  // secret.show is a read the person asks for, so it is called here, on
  // the click, rather than through useRead, which reads as the page loads.
  const reveal = (secret: Secret) => {
    const ask = { id: secret.id };
    asking.current = ask;
    setRevealError(undefined);
    call(SecretShow, { space: secret.space, name: secret.name }).then(
      (shown) => ask === asking.current && setRevealed({ id: secret.id, value: showValue(shown.value_base64) }),
      (error: unknown) => ask === asking.current && setRevealError(error),
    );
  };
  // A Secret written to shows its value no more: it may be out of date.
  const changed = (secret: Secret) => {
    if (revealed?.id === secret.id || asking.current.id === secret.id) {
      hide();
    }
    if (changing?.secret.id === secret.id) {
      setChanging(undefined);
    }
    list.reload();
  };
  const stopChanging = () => setChanging(undefined);

  return (
    <>
      <h1 id={titleId}>{t("secret.list.title")}</h1>
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {revealError !== undefined && <ErrorMessage error={revealError} />}
      {remove.error !== undefined && <ErrorMessage error={remove.error} />}
      {list.data && secrets.length === 0 && <p>{t("secret.list.empty")}</p>}
      {secrets.length > 0 && (
        <table aria-labelledby={titleId}>
          <thead>
            <tr>
              <th>{t("secret.column.space")}</th>
              <th>{t("secret.column.name")}</th>
              <th>{t("secret.column.updated")}</th>
              <th>{t("secret.column.value")}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {secrets.map((secret) => (
              <tr key={secret.id}>
                <td>{secret.space}</td>
                <td>{secret.name}</td>
                <td>{formatDate(secret.updated_at)}</td>
                <td>
                  {revealed?.id === secret.id ? (
                    <ValueShown name={qualified(secret)} value={revealed.value} onHide={hide} />
                  ) : (
                    <button
                      type="button"
                      aria-label={t("secret.show.label", { name: qualified(secret) })}
                      onClick={() => reveal(secret)}
                    >
                      {t("secret.show.button")}
                    </button>
                  )}
                </td>
                <td className="secret-actions">
                  <button
                    type="button"
                    aria-label={t("secret.set.label", { name: qualified(secret) })}
                    onClick={() => setChanging({ secret, action: "set" })}
                  >
                    {t("secret.set.button")}
                  </button>{" "}
                  <button
                    type="button"
                    aria-label={t("secret.rename.label", { name: qualified(secret) })}
                    onClick={() => setChanging({ secret, action: "rename" })}
                  >
                    {t("secret.rename.button")}
                  </button>{" "}
                  <button
                    type="button"
                    aria-label={t("secret.move.label", { name: qualified(secret) })}
                    onClick={() => setChanging({ secret, action: "move" })}
                  >
                    {t("secret.move.button")}
                  </button>{" "}
                  <button
                    type="button"
                    aria-label={t("secret.remove.label", { name: qualified(secret) })}
                    onClick={() => setDeleting(secret)}
                  >
                    {t("secret.remove.button")}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {changing?.action === "set" && (
        <SetValue key={changing.secret.id} secret={changing.secret} onDone={changed} onCancel={stopChanging} />
      )}
      {changing?.action === "rename" && (
        <RenameSecret key={changing.secret.id} secret={changing.secret} onDone={changed} onCancel={stopChanging} />
      )}
      {changing?.action === "move" && (
        <MoveSecret
          key={changing.secret.id}
          secret={changing.secret}
          spaces={spaceNames ?? []}
          onDone={changed}
          onCancel={stopChanging}
        />
      )}
      {deleting && (
        <ConfirmDialog
          title={t("secret.remove.title", { name: qualified(deleting) })}
          consequence={t("secret.remove.consequence", { name: qualified(deleting) })}
          confirm={t("secret.remove.confirm")}
          onCancel={() => setDeleting(undefined)}
          onConfirm={() => {
            setDeleting(undefined);
            void remove.submit({ space: deleting.space, name: deleting.name }).then((removed) => {
              if (removed) {
                changed(deleting);
              }
            });
          }}
        />
      )}
      {spaces.error !== undefined && <ErrorMessage error={spaces.error} />}
      <AddSecret spaces={spaceNames} onAdded={list.reload} />
    </>
  );
}

/**
 * ValueShown is a revealed value: as text, or as its base64 when its bytes
 * are not UTF-8, which a click selects whole, with buttons that copy it and
 * hide it.
 */
function ValueShown({ name, value, onHide }: { name: string; value: ShownValue; onHide: () => void }) {
  const t = useT();
  const [copied, setCopied] = useState<boolean>();
  // A browser without the clipboard API, or one that refuses it, still
  // shows the value, which a click selects whole.
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value.text);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <div role="group" aria-label={t("secret.value.label", { name })}>
      {!value.utf8 && <p className="tag">{t("secret.value.base64")}</p>}
      <code className="secret-value">{value.text}</code>
      <div>
        <button type="button" onClick={() => void copy()}>
          {t("secret.value.copy")}
        </button>{" "}
        <button type="button" onClick={onHide}>
          {t("secret.value.hide")}
        </button>{" "}
        {copied !== undefined && (
          <span role="status">{copied ? t("secret.value.copied") : t("secret.value.copyFailed")}</span>
        )}
      </div>
    </div>
  );
}

/** ChangeProps are what the form of a reversible write to one Secret gets. */
interface ChangeProps {
  secret: Secret;
  /** onDone hears that the write succeeded. */
  onDone: (secret: Secret) => void;
  onCancel: () => void;
}

/**
 * ChangeForm frames the form of one reversible write to a Secret: its
 * title, its field, the button that writes, which never asks first, Cancel,
 * and the refusal, if any.
 */
function ChangeForm({
  title,
  submit,
  ready,
  error,
  onSubmit,
  onCancel,
  children,
}: {
  title: string;
  submit: string;
  ready: boolean;
  error: unknown;
  onSubmit: () => void;
  onCancel: () => void;
  children: ReactNode;
}) {
  const t = useT();
  return (
    <section aria-label={title}>
      <h2>{title}</h2>
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit();
        }}
      >
        {children}{" "}
        <button type="submit" disabled={!ready}>
          {submit}
        </button>{" "}
        <button type="button" onClick={onCancel}>
          {t("secret.change.cancel")}
        </button>
        {error !== undefined && <ErrorMessage error={error} />}
      </form>
    </section>
  );
}

/** SetValue replaces a Secret's value with one typed in; the last writer wins. */
function SetValue({ secret, onDone, onCancel }: ChangeProps) {
  const t = useT();
  const [value, setValue] = useState("");
  const set = useWrite(SecretSet);
  return (
    <ChangeForm
      title={t("secret.set.title", { name: qualified(secret) })}
      submit={t("secret.set.submit")}
      ready={value !== "" && !set.pending}
      error={set.error}
      onSubmit={() =>
        void set
          .submit({ space: secret.space, name: secret.name, value_base64: encodeValue(value) })
          .then((written) => written && onDone(secret))
      }
      onCancel={onCancel}
    >
      <label className="secret-field">
        {t("secret.set.value")}
        <textarea value={value} onChange={(e) => setValue(e.target.value)} autoComplete="off" spellCheck={false} />
      </label>
    </ChangeForm>
  );
}

/** RenameSecret gives a Secret a new name in its Space. */
function RenameSecret({ secret, onDone, onCancel }: ChangeProps) {
  const t = useT();
  const [newName, setNewName] = useState("");
  const rename = useWrite(SecretRename);
  return (
    <ChangeForm
      title={t("secret.rename.title", { name: qualified(secret) })}
      submit={t("secret.rename.submit")}
      ready={newName !== "" && !rename.pending}
      error={rename.error}
      onSubmit={() =>
        void rename
          .submit({ space: secret.space, name: secret.name, new_name: newName })
          .then((written) => written && onDone(secret))
      }
      onCancel={onCancel}
    >
      <label>
        {t("secret.rename.name")}{" "}
        <input value={newName} onChange={(e) => setNewName(e.target.value)} autoComplete="off" spellCheck={false} />
      </label>
    </ChangeForm>
  );
}

/** MoveSecret takes a Secret, under its name, to another Space the caller can act in. */
function MoveSecret({ secret, spaces, onDone, onCancel }: ChangeProps & { spaces: string[] }) {
  const t = useT();
  const [newSpace, setNewSpace] = useState("");
  const move = useWrite(SecretMove);
  return (
    <ChangeForm
      title={t("secret.move.title", { name: qualified(secret) })}
      submit={t("secret.move.submit")}
      ready={newSpace !== "" && !move.pending}
      error={move.error}
      onSubmit={() =>
        void move
          .submit({ space: secret.space, name: secret.name, new_space: newSpace })
          .then((written) => written && onDone(secret))
      }
      onCancel={onCancel}
    >
      <label>
        {t("secret.move.space")}{" "}
        <select value={newSpace} onChange={(e) => setNewSpace(e.target.value)}>
          <option value="">{t("secret.move.choose")}</option>
          {spaces
            .filter((space) => space !== secret.space)
            .map((space) => (
              <option key={space} value={space}>
                {space}
              </option>
            ))}
        </select>
      </label>
    </ChangeForm>
  );
}

/**
 * AddSecret adds a Secret, its value typed in, to a Space the caller can
 * act in: their Personal Space unless they choose another. It never asks.
 */
function AddSecret({ spaces, onAdded }: { spaces: string[] | undefined; onAdded: () => void }) {
  const t = useT();
  const caller = useCaller();
  const [space, setSpace] = useState(caller.user.name);
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const add = useWrite(SecretAdd);
  return (
    <>
      <h2>{t("secret.add.title")}</h2>
      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault();
          void add.submit({ space, name, value_base64: encodeValue(value) }).then((added) => {
            if (added) {
              setName("");
              setValue("");
              onAdded();
            }
          });
        }}
      >
        <label>
          {t("secret.add.space")}{" "}
          <select value={space} onChange={(e) => setSpace(e.target.value)}>
            {(spaces ?? [space]).map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </label>{" "}
        <label>
          {t("secret.add.name")}{" "}
          <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" spellCheck={false} />
        </label>
        <label className="secret-field">
          {t("secret.add.value")}
          <textarea value={value} onChange={(e) => setValue(e.target.value)} autoComplete="off" spellCheck={false} />
        </label>
        <button type="submit" disabled={name === "" || value === "" || add.pending}>
          {t("secret.add.submit")}
        </button>
        {add.error !== undefined && <ErrorMessage error={add.error} />}
      </form>
    </>
  );
}
