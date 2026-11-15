import { Button, buttonVariants } from "@heroui/react";
import { FingerprintPattern } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { Link } from "react-aria-components";

import { AuthPasskeyAdd, AuthPasskeyAddBegin } from "../../api/passkey";
import { AuthLink, AuthPassword, CodeLinkInvalid, CodeLinkUsed } from "../../api/signin";
import { ApiError, post } from "../../client";
import { useCount, useT } from "../../shell/i18n";
import { navigate, signInPage } from "../../shell/router";
import { Form } from "../../ui/Form";
import { notify } from "../../ui/notify";
import { AlertLine, Loading } from "../../ui/states";
import { Door } from "./Door";
import { useLinkSecret } from "./linkSecret";
import { PasswordFields } from "./PasswordFields";
import { refusalText } from "./refusal";
import { cancelled, makePasskey, useCeremony, usePasskeysOffered } from "./webauthn";

/** Step is where a Sign-in Link's page is: reading the link, its choices, a link that no longer works, or a read that failed. */
type Step =
  | { kind: "reading" }
  | { kind: "ready"; user: string; passwordSet: boolean }
  | { kind: "gone"; code: typeof CodeLinkUsed | typeof CodeLinkInvalid }
  | { kind: "failed"; error: unknown };

/** gone is the Step of a refusal that says the link no longer works, or undefined for any other. */
function gone(error: unknown): Step | undefined {
  if (error instanceof ApiError && (error.code === CodeLinkUsed || error.code === CodeLinkInvalid)) {
    return { kind: "gone", code: error.code };
  }
  return undefined;
}

/**
 * LinkPage is a Sign-in Link's page, outside the frame, at /link, and at
 * /password for a link issued before the Sign-in Link (docs/web-design.md,
 * "Sign in and a Sign-in Link's page"). Its route took the link's secret out
 * of the URL's fragment (takingLinkSecret); it asks /auth/link whom the
 * link is for, spending nothing, and shows "For <name>." with two choices:
 * Create a passkey, which makes one through the link
 * (/auth/passkey/add/begin, the browser's ceremony, /auth/passkey/add),
 * and Set a Password instead, which opens the new Password twice, saying it
 * replaces their Password when they have one, for /auth/password. Either
 * spends the link and signs this browser in. A browser without WebAuthn,
 * or a Web URL passkeys do not work at (/auth/methods, usePasskeysOffered),
 * gets the Password's fields at once; the page shows Loading until both
 * /auth/link and /auth/methods have answered. A passkey's answer whose page
 * closed meanwhile says in a toast that this browser is signed in, instead
 * of going on. A link used already or no longer
 * valid replaces the page with what happened and Go to Sign in. The secret
 * is sent only while the page is open: a ceremony whose page closed, as for
 * another link, sends nothing more.
 */
export function LinkPage() {
  // Each link gets a page of its own, so nothing typed for one stays for the next.
  const secret = useLinkSecret();
  return <ForLink key={secret} secret={secret} />;
}

/** ForLink is the page of the link of secret, once the route has taken it. */
function ForLink({ secret }: { secret: string | undefined }) {
  const t = useT();
  const count = useCount();
  const [step, setStep] = useState<Step>({ kind: "reading" });
  const [reads, setReads] = useState(0);
  const [value, setValue] = useState("");
  const [again, setAgain] = useState("");
  const [pending, setPending] = useState(false);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<unknown>();
  const begin = useCeremony();
  const passkeys = usePasskeysOffered();
  // chosen says Set a Password instead was chosen; without passkeys the Password's fields show at once.
  const [chosen, setChosen] = useState(false);
  const password = passkeys === false || chosen;
  // The line saying a Password set here replaces theirs describes the submit, so a screen reader tabbing to it hears it.
  const replaces = useId();

  useEffect(() => {
    if (secret === undefined) {
      return;
    }
    if (secret === "") {
      setStep({ kind: "gone", code: CodeLinkInvalid });
      return;
    }
    let latest = true;
    setStep({ kind: "reading" });
    post(AuthLink, { link: secret }).then(
      ({ user, password_set }) => latest && setStep({ kind: "ready", user, passwordSet: password_set }),
      (failure: unknown) => latest && setStep(gone(failure) ?? { kind: "failed", error: failure }),
    );
    return () => {
      latest = false;
    };
  }, [secret, reads]);

  // Each press begins a ceremony of its own, since the koko server's flow is taken by its first answer.
  const create = async () => {
    const ceremony = begin();
    if (secret === undefined || ceremony === undefined) {
      return;
    }
    setCreating(true);
    setError(undefined);
    try {
      const { flow, options } = await post(AuthPasskeyAddBegin, { link: secret });
      if (!ceremony.open()) {
        return;
      }
      const credential = await makePasskey(options, ceremony.signal);
      if (!ceremony.open()) {
        return;
      }
      const { user } = await post(AuthPasskeyAdd, { flow, credential });
      if (ceremony.open()) {
        navigate("/", { replace: true });
      } else {
        notify(t("passkey.signedIn", { name: user.name }));
      }
    } catch (failure) {
      if (!ceremony.open()) {
        return;
      }
      const next = gone(failure);
      if (next) {
        setStep(next);
      } else if (!cancelled(failure)) {
        setError(failure);
      }
      setCreating(false);
    }
  };

  const submit = async () => {
    if (secret === undefined) {
      return;
    }
    setPending(true);
    setError(undefined);
    try {
      await post(AuthPassword, { link: secret, password: value });
      navigate("/", { replace: true });
    } catch (failure) {
      const next = gone(failure);
      if (next) {
        setStep(next);
      } else {
        setError(failure);
      }
      setPending(false);
    }
  };

  if (step.kind === "gone") {
    const used = step.code === CodeLinkUsed;
    return (
      <Door
        title={t(used ? "signin.linkUsed.title" : "signin.linkInvalid.title")}
        lead={t(used ? "signin.linkUsed.lead" : "signin.linkInvalid.lead")}
      >
        <Link href={signInPage} className={buttonVariants({ variant: "secondary", size: "lg", fullWidth: true })}>
          {t("signin.toSignIn")}
        </Link>
      </Door>
    );
  }
  if (step.kind !== "ready" || passkeys === undefined) {
    return (
      <Door title={t(passkeys === false ? "signin.set.title" : "passkey.link.title")}>
        {step.kind !== "failed" ? (
          <Loading />
        ) : (
          <div className="flex flex-col items-start gap-3">
            <AlertLine>{refusalText(t, count, step.error)}</AlertLine>
            <Button variant="secondary" onPress={() => setReads((n) => n + 1)}>
              {t("shell.retry")}
            </Button>
          </div>
        )}
      </Door>
    );
  }
  const { user, passwordSet } = step;
  if (!password) {
    return (
      <Door title={t("passkey.link.title")} lead={t("signin.set.lead", { name: user })}>
        <div className="flex flex-col gap-3">
          {error !== undefined && <AlertLine>{refusalText(t, count, error)}</AlertLine>}
          <Button size="lg" fullWidth isPending={creating} onPress={() => void create()}>
            <FingerprintPattern aria-hidden className="size-5" />
            {t("passkey.link.create")}
          </Button>
          <Button
            variant="secondary"
            size="lg"
            fullWidth
            onPress={() => {
              setError(undefined);
              setChosen(true);
            }}
          >
            {t("passkey.link.password")}
          </Button>
        </div>
      </Door>
    );
  }
  return (
    <Door title={t("signin.set.title")} lead={t("signin.set.lead", { name: user })}>
      <Form label={t("signin.set.title")} onSubmit={() => void submit()} className="flex flex-col gap-4">
        {passwordSet && (
          <p id={replaces} className="t-label">
            {t("signin.set.replaces", { name: user })}
          </p>
        )}
        {/* The User name, for the browser's password manager to keep with the Password. */}
        <input type="text" name="username" autoComplete="username" value={user} readOnly hidden />
        <PasswordFields user={user} value={value} again={again} onValue={setValue} onAgain={setAgain} />
        {error !== undefined && <AlertLine>{refusalText(t, count, error)}</AlertLine>}
        <Button
          type="submit"
          size="lg"
          fullWidth
          isPending={pending}
          aria-describedby={passwordSet ? replaces : undefined}
          className="mt-2"
        >
          {t("signin.set.submit")}
        </Button>
        {passkeys && (
          <Button
            variant="tertiary"
            fullWidth
            onPress={() => {
              setError(undefined);
              setChosen(false);
            }}
          >
            {t("passkey.link.back")}
          </Button>
        )}
      </Form>
    </Door>
  );
}
