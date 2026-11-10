import { Button } from "@heroui/react";
import { Eye, EyeOff } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState } from "react";

import { type Secret, SecretMove, SecretRemove, SecretRename, SecretSet, SecretShow } from "../../api/secret";
import type { Space } from "../../api/space";
import { call } from "../../client";
import { useWrite } from "../../shell/data";
import { formatAgo } from "../../shell/format";
import { useLanguage, useT } from "../../shell/i18n";
import { useShownOnce } from "../../shell/shownOnce";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { CopyButton } from "../../ui/copy";
import { Field, SearchChoice } from "../../ui/fields";
import { FormDialog } from "../../ui/FormDialog";
import { notify } from "../../ui/notify";
import { RowMenu } from "../../ui/RowMenu";
import { Refusal } from "../../ui/states";
import { encodeValue, type ShownValue, showValue } from "./value";

/** revealFor is how long a value shown stays on the page: 30 seconds. */
export const revealFor = 30_000;

/** qualified names a Secret as every command does: <space>/<secret>. */
export function qualified(secret: Secret): string {
  return `${secret.space}/${secret.name}`;
}

/** useSpaceOptions are the Spaces to pick one from, a Personal Space marked so. */
export function useSpaceOptions(spaces: readonly Space[]) {
  const t = useT();
  return spaces.map((s) => ({
    id: s.name,
    label: s.name,
    description: s.personal ? t("secret.personal") : undefined,
  }));
}

/** fetchValue calls secret.show on a click: the value as the page shows it. */
async function fetchValue(secret: Secret): Promise<ShownValue> {
  const answer = await call(SecretShow, { space: secret.space, name: secret.name });
  return showValue(answer.value_base64);
}

/**
 * useReveal is the value of one Secret, shown in its row once asked for.
 * It is forgotten after Hide, after 30 seconds, when the Secret is written
 * to, when the page is left and when the browser leaves it (useShownOnce);
 * a value secret.show answers after any of those never shows, since it may
 * be out of date or no longer wanted.
 */
function useReveal(secret: Secret) {
  const [value, setValue] = useShownOnce<ShownValue>();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();
  // asking is the one secret.show whose answer may still show: a new
  // object for each Show, and none once the value is forgotten.
  const asking = useRef<object>(undefined);

  const forget = useCallback(() => {
    asking.current = undefined;
    setPending(false);
    setError(undefined);
    setValue(undefined);
  }, [setValue]);

  const show = () => {
    const ask = {};
    asking.current = ask;
    setError(undefined);
    setPending(true);
    fetchValue(secret).then(
      (shown) => {
        if (asking.current === ask) {
          setPending(false);
          setValue(shown);
        }
      },
      (failure: unknown) => {
        if (asking.current === ask) {
          setPending(false);
          setError(failure);
        }
      },
    );
  };

  useEffect(() => {
    if (value === undefined) {
      return;
    }
    const timer = setTimeout(forget, revealFor);
    return () => clearTimeout(timer);
  }, [value, forget]);

  // A write to the Secret, from this row or as a reload finds it, ends what
  // was shown or asked for: it may be the old value.
  useEffect(forget, [secret.updated_at, forget]);

  useEffect(() => {
    window.addEventListener("pagehide", forget);
    return () => window.removeEventListener("pagehide", forget);
  }, [forget]);

  return { value, pending, error, setError, show, forget };
}

/** Acting is the dialog a row's menu opened. */
type Acting = "set" | "rename" | "move" | "delete";

/** SecretRowProps are what a Secret's row shows and does. */
export interface SecretRowProps {
  secret: Secret;
  /** spaces are those the caller can act in, which Move offers; undefined until space.list answers. */
  spaces: readonly Space[] | undefined;
  /** onChanged hears that a write from the row changed or deleted the Secret, so the list loads again. */
  onChanged: () => void;
}

/**
 * SecretRow is a Secret in a list, on the Secrets page and on a Host's
 * page (docs/web-design.md, "Secrets"): its name, when it changed, Copy,
 * Show and its menu. Copy calls secret.show and copies the value without
 * showing it. Show opens the value in the row, in the code face with its
 * own Copy, and becomes Hide; a value that is not UTF-8 shows, and copies,
 * as its base64. The menu sets a new value, renames, moves and deletes the
 * Secret, each in a dialog of its own; only Delete asks first.
 */
export function SecretRow({ secret, spaces, onChanged }: SecretRowProps) {
  const t = useT();
  const language = useLanguage();
  const name = qualified(secret);
  const { value, pending, error, setError, show, forget } = useReveal(secret);
  const [acting, setActing] = useState<Acting>();
  const valueId = useId();
  const toggle = useRef<HTMLButtonElement>(null);
  const focusInValue = useRef(false);
  const others = spaces?.filter((s) => s.name !== secret.space) ?? [];

  // The value leaving the page while the focus is in it, as after 30
  // seconds, gives the focus back to Show rather than to nothing.
  const shown = value !== undefined;
  useEffect(() => {
    if (!shown && focusInValue.current) {
      focusInValue.current = false;
      if (document.activeElement === null || document.activeElement === document.body) {
        toggle.current?.focus();
      }
    }
  }, [shown]);

  const written = (message: string) => {
    forget();
    setActing(undefined);
    notify(message);
    onChanged();
  };

  return (
    <li className="border-b border-line px-5 py-3 last:border-0">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="min-w-0 flex-1 basis-48">
          <p className="t-name truncate">{secret.name}</p>
          <p className="t-label">{t("secret.changed", { ago: formatAgo(secret.updated_at, language) })}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <CopyButton
            variant="tertiary"
            size="sm"
            label={t("secret.copyOf", { name })}
            failed={t("secret.copyFailed")}
            value={async () => {
              setError(undefined);
              return (await fetchValue(secret)).text;
            }}
            onError={setError}
          />
          <Button
            ref={toggle}
            variant="tertiary"
            size="sm"
            isPending={pending}
            aria-expanded={shown}
            aria-controls={shown ? valueId : undefined}
            aria-label={t(shown ? "secret.hideOf" : "secret.showOf", { name })}
            onPress={shown ? forget : show}
          >
            {shown ? <EyeOff aria-hidden className="size-4" /> : <Eye aria-hidden className="size-4" />}
            {t(shown ? "secret.hide" : "secret.show")}
          </Button>
          <RowMenu
            label={t("secret.more", { name })}
            actions={[
              { id: "set", label: t("secret.set"), onAction: () => setActing("set") },
              { id: "rename", label: t("secret.rename"), onAction: () => setActing("rename") },
              {
                id: "move",
                label: t("secret.move"),
                onAction: () => setActing("move"),
                isDisabled: others.length === 0,
              },
              { id: "delete", label: t("secret.delete"), onAction: () => setActing("delete"), danger: true },
            ]}
          />
        </div>
      </div>
      {error !== undefined && (
        <div className="mt-3">
          <Refusal error={error} />
        </div>
      )}
      {value !== undefined && (
        <div
          id={valueId}
          onFocus={() => {
            focusInValue.current = true;
          }}
          onBlur={(event) => {
            focusInValue.current = event.currentTarget.contains(event.relatedTarget);
          }}
          role="group"
          aria-label={t("secret.valueOf", { name })}
          className="mt-3 flex flex-wrap items-start gap-3 rounded-xl border border-border bg-background p-4"
        >
          {!value.utf8 && <p className="t-label basis-full">{t("secret.base64")}</p>}
          <pre
            tabIndex={0}
            className="t-code max-h-56 min-w-0 flex-1 overflow-auto break-all whitespace-pre-wrap select-all"
          >
            {value.text}
          </pre>
          <CopyButton value={value.text} size="sm" />
        </div>
      )}
      {acting === "set" && <SetValue secret={secret} onDone={written} onCancel={() => setActing(undefined)} />}
      {acting === "rename" && <Rename secret={secret} onDone={written} onCancel={() => setActing(undefined)} />}
      {acting === "move" && (
        <Move secret={secret} spaces={others} onDone={written} onCancel={() => setActing(undefined)} />
      )}
      {acting === "delete" && <Delete secret={secret} onDone={written} onCancel={() => setActing(undefined)} />}
    </li>
  );
}

/** ChangeProps are what the dialog of a write to one Secret gets. */
interface ChangeProps {
  secret: Secret;
  /** onDone hears that the write succeeded, with the notice that says what it did. */
  onDone: (message: string) => void;
  onCancel: () => void;
}

/** SetValue replaces a Secret's value with one typed in; the last writer wins. */
function SetValue({ secret, onDone, onCancel }: ChangeProps) {
  const t = useT();
  const [value, setValue] = useState("");
  const set = useWrite(SecretSet);
  const name = qualified(secret);
  return (
    <FormDialog
      title={t("secret.set.title", { name })}
      lead={t("secret.set.lead")}
      submit={t("secret.set")}
      size="md"
      pending={set.pending}
      error={set.error}
      onSubmit={() =>
        void set
          .submit({ space: secret.space, name: secret.name, value_base64: encodeValue(value) })
          .then((done) => done && onDone(t("secret.set.done", { name })))
      }
      onClose={onCancel}
    >
      <ValueField label={t("secret.newValue")} value={value} onChange={setValue} />
    </FormDialog>
  );
}

/**
 * ValueField is the field a value is typed into: several lines in the code
 * face, such as a private key, kept as typed.
 */
export function ValueField({
  label,
  value,
  onChange,
  autoFocus = true,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  autoFocus?: boolean;
}) {
  const t = useT();
  return (
    <Field
      label={label}
      value={value}
      onChange={onChange}
      multiline
      rows={6}
      code
      autoFocus={autoFocus}
      description={t("secret.value.help")}
      validate={(v) => (v === "" ? t("secret.valueRequired") : undefined)}
    />
  );
}

/** Rename gives a Secret a new name in its Space. */
function Rename({ secret, onDone, onCancel }: ChangeProps) {
  const t = useT();
  const [newName, setNewName] = useState(secret.name);
  const rename = useWrite(SecretRename);
  return (
    <FormDialog
      title={t("secret.rename.title", { name: qualified(secret) })}
      submit={t("secret.rename")}
      pending={rename.pending}
      error={rename.error}
      onSubmit={() =>
        void rename
          .submit({ space: secret.space, name: secret.name, new_name: newName })
          .then((done) => done && onDone(t("secret.renamed", { name: newName })))
      }
      onClose={onCancel}
    >
      <Field
        label={t("secret.newName")}
        value={newName}
        onChange={setNewName}
        code
        autoFocus
        validate={(v) => (v === "" ? t("secret.nameRequired") : undefined)}
      />
    </FormDialog>
  );
}

/** Move takes a Secret, under its name, to another Space the caller can act in. */
function Move({ secret, spaces, onDone, onCancel }: ChangeProps & { spaces: readonly Space[] }) {
  const t = useT();
  const [target, setTarget] = useState<string | null>(null);
  const move = useWrite(SecretMove);
  const options = useSpaceOptions(spaces);
  return (
    <FormDialog
      title={t("secret.move.title", { name: qualified(secret) })}
      lead={t("secret.move.lead", { space: secret.space })}
      submit={t("secret.move")}
      pending={move.pending}
      error={move.error}
      onSubmit={() => {
        const space = target ?? "";
        void move
          .submit({ space: secret.space, name: secret.name, new_space: space })
          .then((done) => done && onDone(t("secret.moved", { name: secret.name, space })));
      }}
      onClose={onCancel}
    >
      <SearchChoice
        label={t("secret.moveTo")}
        value={target}
        onChange={setTarget}
        options={options}
        validate={(v) => (v === null ? t("secret.spaceRequired") : undefined)}
      />
    </FormDialog>
  );
}

/** Delete asks first, then deletes the Secret and its value for good. */
function Delete({ secret, onDone, onCancel }: ChangeProps) {
  const t = useT();
  const remove = useWrite(SecretRemove);
  const name = qualified(secret);
  return (
    <ConfirmDialog
      title={t("secret.delete.title", { name })}
      consequence={t("secret.delete.consequence", { name })}
      confirm={t("secret.delete")}
      pending={remove.pending}
      error={remove.error}
      onConfirm={() =>
        void remove
          .submit({ space: secret.space, name: secret.name })
          .then((done) => done && onDone(t("secret.deleted", { name })))
      }
      onCancel={onCancel}
    />
  );
}
