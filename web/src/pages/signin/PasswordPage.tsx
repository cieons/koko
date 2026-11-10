import { Button, buttonVariants } from "@heroui/react";
import { useEffect, useState } from "react";
import { Link } from "react-aria-components";

import { AuthLink, AuthPassword, CodeLinkInvalid, CodeLinkUsed } from "../../api/signin";
import { ApiError, post } from "../../client";
import { useCount, useT } from "../../shell/i18n";
import { navigate, signInPage } from "../../shell/router";
import { Form } from "../../ui/Form";
import { AlertLine, Loading } from "../../ui/states";
import { Door } from "./Door";
import { useLinkSecret } from "./linkSecret";
import { PasswordFields } from "./PasswordFields";
import { refusalText } from "./refusal";

/** Step is where Set a Password is: reading the link, its form, a link that no longer works, or a read that failed. */
type Step =
  | { kind: "reading" }
  | { kind: "form"; user: string }
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
 * PasswordPage is Set a Password, outside the frame, which a Password Link
 * opens (docs/web-design.md, "Sign in and Set a Password"). Its route took
 * the link's secret out of the URL's fragment (takingLinkSecret); it asks
 * /auth/link whom the link is for, spending nothing, and shows "For
 * <name>." with the new Password twice; /auth/password then sets it,
 * spending the link, and signs this browser in. A link used already or no
 * longer valid replaces the form with what happened and Go to Sign in.
 */
export function PasswordPage() {
  // Each link gets a page of its own, so nothing typed for one stays for the next.
  const secret = useLinkSecret();
  return <ForLink key={secret} secret={secret} />;
}

/** ForLink is Set a Password for the link of secret, once the route has taken it. */
function ForLink({ secret }: { secret: string | undefined }) {
  const t = useT();
  const count = useCount();
  const [step, setStep] = useState<Step>({ kind: "reading" });
  const [reads, setReads] = useState(0);
  const [value, setValue] = useState("");
  const [again, setAgain] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();

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
      ({ user }) => latest && setStep({ kind: "form", user }),
      (failure: unknown) => latest && setStep(gone(failure) ?? { kind: "failed", error: failure }),
    );
    return () => {
      latest = false;
    };
  }, [secret, reads]);

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
  if (step.kind !== "form") {
    return (
      <Door title={t("signin.set.title")}>
        {step.kind === "reading" ? (
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
  const { user } = step;
  return (
    <Door title={t("signin.set.title")} lead={t("signin.set.lead", { name: user })}>
      <Form label={t("signin.set.title")} onSubmit={() => void submit()} className="flex flex-col gap-4">
        {/* The User name, for the browser's password manager to keep with the Password. */}
        <input type="text" name="username" autoComplete="username" value={user} readOnly hidden />
        <PasswordFields user={user} value={value} again={again} onValue={setValue} onAgain={setAgain} />
        {error !== undefined && <AlertLine>{refusalText(t, count, error)}</AlertLine>}
        <Button type="submit" size="lg" fullWidth isPending={pending} className="mt-2">
          {t("signin.set.submit")}
        </Button>
      </Form>
    </Door>
  );
}
