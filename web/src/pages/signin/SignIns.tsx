import { Button, Chip, Switch } from "@heroui/react";
import { MonitorSmartphone } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";

import { type SignIn, SignInList, SignInRevoke } from "../../api/signin";
import type { MessageKey } from "../../i18n/catalogues";
import { browserName } from "../../shell/browserName";
import { type Caller, useCaller } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { formatAgo, formatDate } from "../../shell/format";
import { type Translate, useLanguage, useT } from "../../shell/i18n";
import { navigate, signInPage } from "../../shell/router";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { useFocusFallback } from "../../ui/focus";
import { notify } from "../../ui/notify";
import { Loaded } from "../../ui/states";

/** SignInsProps are whose Sign-ins a list shows, and where. */
export interface SignInsProps {
  /** user names the User whose Sign-ins the list shows. */
  user: string;
  /** level is the heading's level: 2 on a page, 3 in a panel. */
  level: 2 | 3;
  /**
   * changes counts what the page did that ends Sign-ins, such as a change
   * of Password; each new count reads the list again, keeping its view.
   */
  changes?: number;
  /** acts offers Revoke: false where the caller may not act on user, as an Administrator on the Owner. */
  acts?: boolean;
  className?: string;
}

/**
 * SignIns lists one User's Browser Sign-ins (signin.list naming them), the
 * ended ones when asked for, this browser's marked "This browser"; and
 * revokes a live one once the caller confirms (signin.revoke). Revoking
 * this browser's own signs it out, to Sign in. Account shows the caller's
 * own; a person's panel, the Owner's and the Administrators' alone, shows
 * that person's, without Revoke where the caller may not act on them.
 */
export function SignIns({ user, level, changes = 0, acts = true, className = "" }: SignInsProps) {
  const t = useT();
  const language = useLanguage();
  const caller = useCaller();
  const id = useId();
  const [all, setAll] = useState(false);
  const list = useRead(SignInList, all ? { user, all: true } : { user });
  const [revoking, setRevoking] = useState<SignIn>();
  const { reload } = list;
  const seen = useRef(changes);
  useEffect(() => {
    if (changes !== seen.current) {
      seen.current = changes;
      reload();
    }
  }, [changes, reload]);
  const Heading = level === 2 ? "h2" : "h3";
  const [heading, refocus] = useFocusFallback<HTMLHeadingElement>(list.data);
  return (
    <section aria-labelledby={id} className={`flex flex-col gap-3 ${className}`}>
      <Heading id={id} ref={heading} tabIndex={-1} className="t-heading flex min-h-10 items-center gap-2 outline-none">
        <MonitorSmartphone aria-hidden className="size-5 text-muted" />
        {t("signin.signins.title")}
      </Heading>
      <Loaded read={list}>
        {({ signins }) =>
          signins.length === 0 ? (
            <p className="text-muted">{t("signin.signins.none", { name: user })}</p>
          ) : (
            <ul className="flex flex-col">
              {signins.map((s) => {
                const live = s.ended_at === undefined;
                return (
                  <li key={s.id} className="flex items-center justify-between gap-4 border-t border-line py-3">
                    <div className="min-w-0">
                      <p className="flex flex-wrap items-center gap-2">
                        <span className={`t-name ${live ? "" : "text-muted line-through"}`}>{browserName(t, s)}</span>
                        {s.id === caller.signin?.id && (
                          <Chip color="accent" variant="soft">
                            {t("signin.signins.here")}
                          </Chip>
                        )}
                        {!live && <Chip variant="secondary">{t("signin.signins.ended")}</Chip>}
                      </p>
                      <p className="t-label">
                        <span className="t-code">{s.address}</span>
                        {" · "}
                        {live
                          ? `${t("device.lastUsed", { ago: formatAgo(s.last_used_at, language) })} · ${t("signin.signins.ends", { date: formatDate(s.expires_at) })}`
                          : t("signin.signins.endedOn", {
                              date: formatDate(s.ended_at ?? ""),
                              reason: endReason(t, s.end_reason),
                            })}
                      </p>
                    </div>
                    {live && acts && (
                      <Button
                        variant="danger-soft"
                        aria-label={t("signin.revoke.title", { client: browserName(t, s) })}
                        onPress={() => setRevoking(s)}
                      >
                        {t("signin.revoke")}
                      </Button>
                    )}
                  </li>
                );
              })}
            </ul>
          )
        }
      </Loaded>
      <Switch isSelected={all} onChange={setAll} size="sm">
        <Switch.Content>
          <Switch.Control>
            <Switch.Thumb />
          </Switch.Control>
          {t("signin.signins.showEnded")}
        </Switch.Content>
      </Switch>
      {revoking && (
        <RevokeSignIn
          signin={revoking}
          onDone={() => {
            setRevoking(undefined);
            refocus();
            list.reload();
          }}
          onRefused={list.reload}
          onCancel={() => setRevoking(undefined)}
        />
      )}
    </section>
  );
}

/** endReasons are the message of each reason a Sign-in ended (api.EndSignedOut and the rest). */
const endReasons: Readonly<Record<string, MessageKey>> = {
  signed_out: "signin.end.signed_out",
  expired: "signin.end.expired",
  revoked: "signin.end.revoked",
  password_changed: "signin.end.password_changed",
  password_removed: "signin.end.password_removed",
  user_removed: "signin.end.user_removed",
};

/** endReason says why a Sign-in ended; a reason this web UI does not know is shown as the koko server named it. */
function endReason(t: Translate, reason: string | undefined): string {
  const key = reason === undefined ? undefined : endReasons[reason];
  return key === undefined ? (reason ?? "") : t(key);
}

/**
 * revokeConsequence states what revoking signin does: this browser's own
 * signs it out; another of the caller's signs that browser out; another
 * User's, the Owner's to revoke, signs that User out of that browser.
 */
function revokeConsequence(t: Translate, signin: SignIn, caller: Caller): string {
  if (signin.id === caller.signin?.id) {
    return t("signin.revoke.consequenceHere");
  }
  if (signin.user !== caller.user.name) {
    return t("signin.revoke.consequenceOther", { name: signin.user });
  }
  return t("signin.revoke.consequence");
}

/**
 * RevokeSignIn asks before it revokes a live Sign-in, stating what follows.
 * Revoking this browser's own takes it to Sign in, since every later call
 * would be refused. A refusal shows in the dialog and reloads the list
 * (onRefused), so one that ended meanwhile shows as ended.
 */
function RevokeSignIn({
  signin,
  onDone,
  onRefused,
  onCancel,
}: {
  signin: SignIn;
  onDone: () => void;
  onRefused: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const caller = useCaller();
  const revoke = useWrite(SignInRevoke);
  const here = signin.id === caller.signin?.id;
  return (
    <ConfirmDialog
      title={t("signin.revoke.title", { client: browserName(t, signin) })}
      consequence={revokeConsequence(t, signin, caller)}
      confirm={t("signin.revoke")}
      pending={revoke.pending}
      error={revoke.error}
      onCancel={onCancel}
      onConfirm={() =>
        void revoke.submit({ id: signin.id }).then((revoked) => {
          if (!revoked) {
            onRefused();
          } else if (here) {
            navigate(signInPage, { replace: true });
          } else {
            notify(t("signin.revokedDone", { client: browserName(t, signin) }));
            onDone();
          }
        })
      }
    />
  );
}
