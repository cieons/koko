import { Button } from "@heroui/react";
import { useId, useState } from "react";

import { type Identity, IdentityList, IdentityUnlink } from "../../api/identity";
import type { User } from "../../api/user";
import { useCaller } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { formatAgo, formatDate } from "../../shell/format";
import { type Translate, useCount, useLanguage, useT } from "../../shell/i18n";
import { navigate, signInPage } from "../../shell/router";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { useFocusFallback } from "../../ui/focus";
import { notify } from "../../ui/notify";
import { AlertLine, Loaded } from "../../ui/states";
import { continueWithProvider } from "./provider";
import { refusalText } from "./refusal";
import { useCeremony } from "./webauthn";

/** LinkedIdentityProps are whose Linked Identity the section shows, and what it offers. */
export interface LinkedIdentityProps {
  /** user is the User whose Linked Identity it is, as user.current or user.list answers them. */
  user: User;
  /**
   * provider is the Identity Provider's name, which titles the section:
   * null while none is configured, undefined where the page cannot ask,
   * through koko web or for the Local caller, or, for a browser signed
   * in, until /auth/methods answers.
   */
  provider: string | null | undefined;
  /** level is the heading's level: 3 in a panel on a page, 4 in a section of a side panel. */
  level: 3 | 4;
  /** acts offers Unlink: false where the caller may not act on user, as an Administrator on the Owner. */
  acts: boolean;
  /** link offers Link {name} while user has none: for a browser signed in, on Account. */
  link: boolean;
  /** refused is the refusal the provider's callback came back to the page with, shown until the next try. */
  refused?: unknown;
  /** onUnlinked hears the identity unlinked, so the page reads the User again. */
  onUnlinked: () => void;
}

/**
 * LinkedIdentity is one User's Linked Identity (identity.list, read while
 * user.identity_linked_at says they have one), in a section titled by the
 * Identity Provider's name: "Linked as <label>", who issued the Sign-in Link
 * it came through, when it was linked and last used, and Unlink, which asks
 * first. Without one, and where link says so, Link <name> begins linking it
 * to this browser's Sign-in and sends the browser to the provider. With
 * neither an identity, a provider to link nor a refusal to show, there is
 * no section; one that lost its identity says there is none. A browser
 * signed in shows it once /auth/methods has named the provider or none.
 * Unlinking the identity this browser signed in through goes to Sign in.
 */
export function LinkedIdentity({ user, provider, level, acts, link, refused, onUnlinked }: LinkedIdentityProps) {
  const t = useT();
  const count = useCount();
  const language = useLanguage();
  const caller = useCaller();
  const id = useId();
  const begin = useCeremony();
  const own = user.name === caller.user.name;
  const linked = user.identity_linked_at !== undefined;
  // One's own is read and unlinked without naming the User, as Account's calls are.
  const request = own ? {} : { user: user.name };
  const list = useRead(IdentityList, request, { enabled: linked });
  const [error, setError] = useState<unknown>(refused);
  const [pending, setPending] = useState(false);
  const [unlinking, setUnlinking] = useState<Identity>();
  const [heading, refocus] = useFocusFallback<HTMLHeadingElement>(linked);
  // A section once shown stays, saying there is none, so an Unlink's focus has its heading to go to.
  const [shown, setShown] = useState(linked);
  if (linked && !shown) {
    setShown(true);
  }
  const linkable = link && !linked && typeof provider === "string";
  // A browser signed in waits for /auth/methods, so the title never changes from Linked identity to the name.
  const waiting = caller.signin !== undefined && provider === undefined;
  if (waiting || (!shown && !linkable && error === undefined)) {
    return null;
  }
  const Heading = level === 3 ? "h3" : "h4";

  // A begin answered after the page closed goes nowhere.
  const withProvider = async () => {
    const page = begin();
    if (page === undefined) {
      return;
    }
    setPending(true);
    setError(undefined);
    try {
      await continueWithProvider({ purpose: "link" }, page.open);
    } catch (failure) {
      if (page.open()) {
        setError(failure);
        setPending(false);
      }
    }
  };

  return (
    <section aria-labelledby={id} className="flex flex-col gap-3">
      <Heading
        id={id}
        ref={heading}
        tabIndex={-1}
        className={`${level === 3 ? "t-heading" : "t-label font-semibold text-foreground"} outline-none`}
      >
        {provider ?? t("identity.linked.title")}
      </Heading>
      {error !== undefined && <AlertLine>{refusalText(t, count, error)}</AlertLine>}
      {linked && (
        <Loaded read={list}>
          {({ identities }) =>
            identities.length === 0 ? (
              <p className="text-muted">{t("identity.noIdentity")}</p>
            ) : (
              <ul className="flex flex-col">
                {identities.map((i) => (
                  <li key={i.id} className="flex items-center justify-between gap-4 border-t border-line py-3">
                    <div className="min-w-0">
                      <p className="t-name break-words">{t("identity.linkedAs", { label: i.label })}</p>
                      <p className="t-label">
                        {[
                          ...(i.linked_by === null ? [] : [t("identity.byLink", { by: i.linked_by })]),
                          t("identity.linkedOn", { date: formatDate(i.linked_at) }),
                          i.last_used_at === null
                            ? t("passkey.neverUsed")
                            : t("device.lastUsed", { ago: formatAgo(i.last_used_at, language) }),
                        ].join(" · ")}
                      </p>
                    </div>
                    {acts && (
                      <Button variant="danger-soft" onPress={() => setUnlinking(i)}>
                        {t("identity.unlink")}
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
            )
          }
        </Loaded>
      )}
      {!linked && !linkable && shown && <p className="text-muted">{t("identity.noIdentity")}</p>}
      {linkable && (
        <div>
          <Button variant="secondary" isPending={pending} onPress={() => void withProvider()}>
            {t("identity.link", { name: provider })}
          </Button>
        </div>
      )}
      {unlinking && (
        <Unlink
          identity={unlinking}
          request={request}
          last={user.password_set_at === undefined && (user.passkeys ?? 0) === 0}
          onDone={() => {
            setUnlinking(undefined);
            refocus();
            onUnlinked();
          }}
          onRefused={list.reload}
          onCancel={() => setUnlinking(undefined)}
        />
      )}
    </section>
  );
}

/**
 * unlinkConsequence states what unlinking identity does: the browsers
 * signed in through it are signed out, this one among them when its
 * Sign-in was made through it; and, for the User's last way to sign in,
 * that their next sign-in needs a Sign-in Link.
 */
function unlinkConsequence(t: Translate, identity: Identity, here: boolean, last: boolean): string {
  const name = identity.user;
  if (last) {
    return t(here ? "identity.unlink.consequenceHereLast" : "identity.unlink.consequenceLast", { name });
  }
  return t(here ? "identity.unlink.consequenceHere" : "identity.unlink.consequence", { name });
}

/**
 * Unlink asks before it unlinks a Linked Identity (identity.unlink),
 * stating what follows. Unlinking the identity this browser's Sign-in was
 * made through takes it to Sign in, since every later call would be
 * refused. A refusal shows in the dialog and reads the identity again
 * (onRefused).
 */
function Unlink({
  identity,
  request,
  last,
  onDone,
  onRefused,
  onCancel,
}: {
  identity: Identity;
  request: { user?: string };
  last: boolean;
  onDone: () => void;
  onRefused: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const caller = useCaller();
  const unlink = useWrite(IdentityUnlink);
  const here = caller.signin?.method === "identity" && caller.signin.credential === identity.id;
  return (
    <ConfirmDialog
      title={t("identity.unlink.title", { label: identity.label })}
      consequence={unlinkConsequence(t, identity, here, last)}
      confirm={t("identity.unlink")}
      pending={unlink.pending}
      error={unlink.error}
      onCancel={onCancel}
      onConfirm={() =>
        void unlink.submit(request).then((unlinked) => {
          if (!unlinked) {
            onRefused();
          } else if (here) {
            navigate(signInPage, { replace: true });
          } else {
            notify(t("identity.unlink.done", { label: identity.label }));
            onDone();
          }
        })
      }
    />
  );
}
