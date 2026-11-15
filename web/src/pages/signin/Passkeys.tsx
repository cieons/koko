import { Button, Chip } from "@heroui/react";
import { Plus } from "lucide-react";
import { useId, useState } from "react";

import { AuthPasskeyAdd, AuthPasskeyAddBegin, type Passkey, PasskeyList, PasskeyRemove } from "../../api/passkey";
import { CodeInvalidName } from "../../api/errors";
import { ApiError, post } from "../../client";
import { browserName } from "../../shell/browserName";
import { useCaller } from "../../shell/caller";
import { reloadReads, useRead, useWrite } from "../../shell/data";
import { formatAgo, formatDate } from "../../shell/format";
import { type Count, type Translate, useCount, useLanguage, useT } from "../../shell/i18n";
import { navigate, signInPage } from "../../shell/router";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { Field } from "../../ui/fields";
import { useFocusFallback } from "../../ui/focus";
import { Form } from "../../ui/Form";
import { notify, notifyRefusal } from "../../ui/notify";
import { AlertLine, Loaded } from "../../ui/states";
import { refusalText } from "./refusal";
import { cancelled, makePasskey, useCeremony } from "./webauthn";

/** maxNameLength is the longest name a passkey takes, in characters (api.MaxPasskeyNameLength). */
const maxNameLength = 64;

/** PasskeysProps are whose passkeys a list shows, and what it offers. */
export interface PasskeysProps {
  /** user names the User whose Passkeys the list shows (passkey.list naming them). */
  user: string;
  /**
   * webHost is the Web URL's host without its port, beside which a passkey
   * made for another host says so: null while the Web URL is unset,
   * undefined until server.show answers (useWebURL).
   */
  webHost: string | null | undefined;
  /** level is the heading's level: 3 in a panel on a page, 4 in a section of a side panel. */
  level: 3 | 4;
  /** acts offers Remove: false where the caller may not act on user, as an Administrator on the Owner. */
  acts: boolean;
  /**
   * otherWays says user holds a Password or a Linked Identity, so that
   * removing their one passkey leaves them a way to sign in.
   */
  otherWays: boolean;
  /**
   * add is what the list offers for adding one: "here", Add a passkey,
   * for a browser signed in with WebAuthn; "elsewhere", a sentence saying
   * passkeys are added in a browser at the Web URL, through koko web or for
   * the Local caller; "none", nothing, as in a person's panel.
   */
  add: "here" | "elsewhere" | "none";
}

/**
 * Passkeys lists one User's passkeys (passkey.list): each one's name, the
 * host it was made for where that is not the Web URL's, Synced, when it
 * was added and last used; and Remove, which asks first. On Account it adds
 * one too (AddPasskey). After a removal every read on the page is read
 * again, as the Sign-ins it ended and the User's count change; removing the
 * passkey this browser signed in with goes to Sign in.
 */
export function Passkeys({ user, webHost, level, acts, otherWays, add }: PasskeysProps) {
  const t = useT();
  const language = useLanguage();
  const id = useId();
  const list = useRead(PasskeyList, { user });
  const [removing, setRemoving] = useState<Passkey>();
  const [heading, refocus] = useFocusFallback<HTMLHeadingElement>(list.data);
  const Heading = level === 3 ? "h3" : "h4";
  const count = list.data?.passkeys.length ?? 0;
  return (
    <section aria-labelledby={id} className="flex flex-col gap-3">
      <Heading
        id={id}
        ref={heading}
        tabIndex={-1}
        className={`${level === 3 ? "t-heading" : "t-label font-semibold text-foreground"} outline-none`}
      >
        {t("passkey.title")}
      </Heading>
      <Loaded read={list}>
        {({ passkeys }) =>
          passkeys.length === 0 ? (
            <p className="text-muted">{t("passkey.none")}</p>
          ) : (
            <ul className="flex flex-col">
              {passkeys.map((p) => (
                <li key={p.id} className="flex items-center justify-between gap-4 border-t border-line py-3">
                  <div className="min-w-0">
                    <p className="flex flex-wrap items-center gap-2">
                      <span className="t-name break-words">{p.name}</span>
                      {p.synced && <Chip variant="secondary">{t("passkey.synced")}</Chip>}
                    </p>
                    <p className="t-label">
                      {[
                        ...(webHost !== undefined && p.host !== webHost
                          ? [t("passkey.madeFor", { host: p.host })]
                          : []),
                        t("user.added", { date: formatDate(p.added_at) }),
                        p.last_used_at === null
                          ? t("passkey.neverUsed")
                          : t("device.lastUsed", { ago: formatAgo(p.last_used_at, language) }),
                      ].join(" · ")}
                    </p>
                  </div>
                  {acts && (
                    <Button
                      variant="danger-soft"
                      aria-label={t("passkey.remove.label", { name: p.name })}
                      onPress={() => setRemoving(p)}
                    >
                      {t("passkey.remove")}
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )
        }
      </Loaded>
      {add === "here" && <AddPasskey onAdded={reloadReads} />}
      {add === "elsewhere" && <p className="t-label">{t("passkey.viaBrowser")}</p>}
      {removing && (
        <RemovePasskey
          passkey={removing}
          last={!otherWays && count === 1}
          onDone={() => {
            setRemoving(undefined);
            refocus();
            reloadReads();
          }}
          onRefused={list.reload}
          onCancel={() => setRemoving(undefined)}
        />
      )}
    </section>
  );
}

/**
 * removeConsequence states what removing passkey does: the browsers signed
 * in with it are signed out, this one among them when its Sign-in was made
 * with it; for the User's last way to sign in, that their next sign-in
 * needs a Sign-in Link; and that the device keeps the passkey.
 */
function removeConsequence(t: Translate, passkey: Passkey, here: boolean, last: boolean): string {
  const name = passkey.user;
  if (last) {
    return t(here ? "passkey.remove.consequenceHereLast" : "passkey.remove.consequenceLast", { name });
  }
  return t(here ? "passkey.remove.consequenceHere" : "passkey.remove.consequence", { name });
}

/**
 * RemovePasskey asks before it removes a passkey (passkey.remove), stating
 * what follows. Removing the passkey this browser's Sign-in was made with
 * takes it to Sign in, since every later call would be refused. A refusal
 * shows in the dialog and reads the list again (onRefused).
 */
function RemovePasskey({
  passkey,
  last,
  onDone,
  onRefused,
  onCancel,
}: {
  passkey: Passkey;
  last: boolean;
  onDone: () => void;
  onRefused: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const caller = useCaller();
  const remove = useWrite(PasskeyRemove);
  const here = caller.signin !== undefined && caller.signin.credential === passkey.id;
  return (
    <ConfirmDialog
      title={t("passkey.remove.title", { name: passkey.name })}
      consequence={removeConsequence(t, passkey, here, last)}
      confirm={t("passkey.remove")}
      pending={remove.pending}
      error={remove.error}
      onCancel={onCancel}
      onConfirm={() =>
        void remove.submit({ id: passkey.id }).then((removed) => {
          if (!removed) {
            onRefused();
          } else if (here) {
            navigate(signInPage, { replace: true });
          } else {
            notify(t("passkey.remove.done", { name: passkey.name }));
            onDone();
          }
        })
      }
    />
  );
}

/**
 * AddPasskey adds a passkey from this browser's Sign-in: a name, prefilled
 * with this browser's label and checked on submit, then
 * /auth/passkey/add/begin, the browser's ceremony and /auth/passkey/add,
 * which keeps the Sign-in. A ceremony the person cancelled shows nothing; a
 * refusal shows in place, the name kept; onAdded hears the passkey added.
 * Nothing more is sent once Account has closed, and the outcome of an
 * answer sent before it closed is said in a toast.
 */
function AddPasskey({ onAdded }: { onAdded: () => void }) {
  const t = useT();
  const count = useCount();
  const caller = useCaller();
  const begin = useCeremony();
  const label = caller.signin ? browserName(t, caller.signin) : t("shell.browser.unknown");
  const [name, setName] = useState(label);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();

  // Each press begins a ceremony of its own, since the koko server's flow is taken by its first answer.
  const submit = async () => {
    const ceremony = begin();
    if (ceremony === undefined) {
      return;
    }
    setPending(true);
    setError(undefined);
    // sent says the answer went to the koko server, whose outcome a closed Account still says.
    let sent = false;
    try {
      const { flow, options } = await post(AuthPasskeyAddBegin, {});
      if (!ceremony.open()) {
        return;
      }
      const credential = await makePasskey(options, ceremony.signal);
      if (!ceremony.open()) {
        return;
      }
      sent = true;
      const { passkey } = await post(AuthPasskeyAdd, { flow, credential, name: name.trim() });
      notify(t("passkey.add.done", { name: passkey.name }));
      onAdded();
      if (ceremony.open()) {
        setName(label);
        setPending(false);
      }
    } catch (failure) {
      if (ceremony.open()) {
        setError(cancelled(failure) ? undefined : failure);
        setPending(false);
      } else if (sent) {
        notifyRefusal(addRefusal(t, count, failure));
      }
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <Form label={t("passkey.add")} onSubmit={() => void submit()} className="flex items-start gap-3">
        <Field
          label={t("passkey.add.name")}
          value={name}
          onChange={setName}
          className="min-w-0 flex-1"
          validate={(v) =>
            v.trim() === ""
              ? t("passkey.add.nameRequired")
              : [...v.trim()].length > maxNameLength
                ? t("passkey.add.nameTooLong")
                : undefined
          }
        />
        <Button type="submit" variant="secondary" className="mt-6 shrink-0" isPending={pending}>
          <Plus aria-hidden className="size-4" />
          {t("passkey.add")}
        </Button>
      </Form>
      {error !== undefined && <AlertLine>{addRefusal(t, count, error)}</AlertLine>}
    </div>
  );
}

/** addRefusal says why adding a passkey failed: invalid_name as the passkey's own rule, not a Host's; else as signing in says it. */
function addRefusal(t: Translate, count: Count, error: unknown): string {
  return error instanceof ApiError && error.code === CodeInvalidName
    ? t("passkey.add.nameInvalid")
    : refusalText(t, count, error);
}
