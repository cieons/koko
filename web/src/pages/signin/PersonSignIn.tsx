import { Button } from "@heroui/react";
import { KeyRound } from "lucide-react";
import { useState } from "react";

import { type PasswordLink, PasswordLinkIssue, PasswordRemove } from "../../api/signin";
import type { User } from "../../api/user";
import { useCaller } from "../../shell/caller";
import { useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { useT } from "../../shell/i18n";
import { useShownOnce } from "../../shell/shownOnce";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { useFocusFallback } from "../../ui/focus";
import { notify } from "../../ui/notify";
import {
  defaultValidFor,
  IssueDialog,
  SignInLinkLine,
  type ValidFor,
  ValidForChoice,
  validForRequest,
} from "../device/IssueDialog";
import { LinkedIdentity } from "./LinkedIdentity";
import { Passkeys } from "./Passkeys";
import { useWebURL } from "./webURL";

/**
 * PersonSignIn is a person's ways to sign in, in their panel, the Owner's
 * and the Administrators' alone (docs/web-design.md, "Access"): their
 * Password, set on which date, or none, with Remove Password, asking
 * first, while there is one; their passkeys (Passkeys), each with Remove;
 * their Linked Identity (LinkedIdentity), with Unlink, titled by the
 * Identity Provider's name (provider) where the page knows it; and Issue
 * Sign-in Link, which needs the Web URL (server.show) and is disabled with
 * the reason while it is unset. The actions show only where the caller may
 * act on the person (acts). onChanged hears a Password removed or the
 * identity unlinked, so the panel and Access show it; the section's
 * heading then takes the focus Remove Password had.
 */
export function PersonSignIn({
  user,
  acts,
  provider,
  onChanged,
}: {
  user: User;
  acts: boolean;
  provider: string | null | undefined;
  onChanged: () => void;
}) {
  const t = useT();
  const webURL = useWebURL();
  const [dialog, setDialog] = useState<"issue" | "remove">();
  const set = user.password_set_at;
  const [heading, refocus] = useFocusFallback<HTMLHeadingElement>(set);
  return (
    <section className="flex flex-col gap-3">
      <h3 ref={heading} tabIndex={-1} className="t-heading flex min-h-10 items-center gap-2 outline-none">
        <KeyRound aria-hidden className="size-5 text-muted" />
        {t("signin.methods")}
      </h3>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-muted">
          {set === undefined
            ? t("signin.person.none")
            : t("signin.person.setOn", { date: formatDate(set) })}
        </p>
        {acts && set !== undefined && (
          <Button variant="danger-soft" onPress={() => setDialog("remove")}>
            {t("signin.remove")}
          </Button>
        )}
      </div>
      <Passkeys
        user={user.name}
        webHost={webURL.host}
        level={4}
        acts={acts}
        otherWays={set !== undefined || user.identity_linked_at !== undefined}
        add="none"
      />
      <LinkedIdentity user={user} provider={provider} level={4} acts={acts} link={false} onUnlinked={onChanged} />
      {acts && (
        <div className="flex flex-col items-start gap-2">
          {webURL.reason !== undefined && <p className="t-label">{webURL.reason(t)}</p>}
          <Button variant="secondary" isDisabled={!webURL.set} onPress={() => setDialog("issue")}>
            {t("signin.link.issue")}
          </Button>
        </div>
      )}
      {dialog === "issue" && <IssueLink user={user} onClose={() => setDialog(undefined)} />}
      {dialog === "remove" && (
        <RemovePassword
          name={user.name}
          onRemoved={() => {
            setDialog(undefined);
            refocus();
            onChanged();
          }}
          onCancel={() => setDialog(undefined)}
        />
      )}
    </section>
  );
}

/**
 * IssueLink issues a Sign-in Link for user, valid for the time chosen, and
 * shows it once; it never asks first. A newer link voids the User's earlier
 * unused one, an Invite's included, which its lead says.
 */
function IssueLink({ user, onClose }: { user: User; onClose: () => void }) {
  const t = useT();
  const [validFor, setValidFor] = useState<ValidFor>(defaultValidFor);
  // The link shown once, with the validity it was issued for.
  const [issued, setIssued] = useShownOnce<{ link: PasswordLink; validFor: ValidFor }>();
  // done keeps the second step once the link is issued, so a link the browser forgot is not offered again.
  const [done, setDone] = useState(false);
  const issue = useWrite(PasswordLinkIssue);
  return (
    <IssueDialog
      title={t("signin.link.title", { name: user.name })}
      lead={t("signin.link.lead")}
      submit={t("signin.link.issue")}
      pending={issue.pending}
      changed={validFor !== defaultValidFor}
      error={issue.error}
      doneTitle={t("signin.link.ready")}
      issued={
        !done ? undefined : issued ? (
          <SignInLinkLine link={issued.link} validFor={issued.validFor} expired={t("signin.link.expired")} />
        ) : (
          <p>{t("signin.link.forgotten")}</p>
        )
      }
      onSubmit={() =>
        void issue.submit({ user: user.name, ...validForRequest(validFor) }).then((answer) => {
          if (answer) {
            setIssued({ link: answer.password_link, validFor });
            setDone(true);
          }
        })
      }
      onClose={() => {
        setIssued(undefined);
        onClose();
      }}
    >
      <ValidForChoice value={validFor} onChange={setValidFor} />
    </IssueDialog>
  );
}

/**
 * RemovePassword removes a User's Password, asking first and saying what
 * follows: the Sign-ins made with the Password end and an unused Sign-in
 * Link stops working, the User, their devices and their other ways to sign
 * in staying; for one's own, from a browser signed in with it, that this
 * browser is signed out too, as the shell then takes it to Sign in. A
 * browser signed in another way stays. no_password shows in place.
 */
function RemovePassword({ name, onRemoved, onCancel }: { name: string; onRemoved: () => void; onCancel: () => void }) {
  const t = useT();
  const caller = useCaller();
  const remove = useWrite(PasswordRemove);
  const here = caller.signin?.method === "password" && caller.user.name === name;
  return (
    <ConfirmDialog
      title={t("signin.remove.title", { name })}
      consequence={t(here ? "signin.remove.consequenceHere" : "signin.remove.consequence", { name })}
      confirm={t("signin.remove")}
      pending={remove.pending}
      error={remove.error}
      onCancel={onCancel}
      onConfirm={() =>
        void remove.submit({ user: name }).then((removed) => {
          if (removed) {
            notify(t("signin.remove.done", { name }));
            onRemoved();
          }
        })
      }
    />
  );
}
