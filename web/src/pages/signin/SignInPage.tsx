import { Button, Separator } from "@heroui/react";
import { FingerprintPattern, LogIn } from "lucide-react";
import { useState } from "react";

import { AuthPasskeySignIn, AuthPasskeySignInBegin } from "../../api/passkey";
import { AuthSignIn } from "../../api/signin";
import { post } from "../../client";
import { useCount, useT } from "../../shell/i18n";
import { linkPage, navigate, passwordPage, signInPage, useSearchParams } from "../../shell/router";
import { Field } from "../../ui/fields";
import { Form } from "../../ui/Form";
import { notify } from "../../ui/notify";
import { AlertLine, Loading } from "../../ui/states";
import { Door } from "./Door";
import { useMethods } from "./methods";
import { continueWithProvider, refusedFrom } from "./provider";
import { refusalText } from "./refusal";
import { cancelled, getPasskey, useCeremony } from "./webauthn";

/**
 * SignInPage is Sign in, outside the frame (docs/web-design.md, "Sign in
 * and a Sign-in Link's page"): Sign in with a passkey first, where the
 * browser has WebAuthn and /auth/methods says passkeys work at the Web URL,
 * and Continue with the Identity Provider where /auth/methods names one
 * (useMethods), then a divider and a User name and Password, posted to
 * /auth/signin. The page shows Loading until /auth/methods answers, so no
 * layout shows and then changes. A passkey or a Password sets the Sign-in
 * cookie, then goes on to the page next names, on this origin only, or the
 * first place; a passkey's answer whose page closed meanwhile says in a
 * toast that this browser is signed in, instead. Continue begins a sign-in
 * through the provider, with next, and sends the browser there; its
 * callback comes back here with ?refused=<code> when it fails. A refusal,
 * the callback's among them, shows above everything, and
 * too_many_attempts says in how many minutes to try again; a passkey's
 * ceremony the person cancelled shows nothing. One line says the Owner or
 * an Administrator can send a Sign-in Link, for a lost way to sign in.
 */
export function SignInPage() {
  const t = useT();
  const count = useCount();
  const [params] = useSearchParams();
  const begin = useCeremony();
  const methods = useMethods();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState<"password" | "passkey" | "provider">();
  // The callback's refusal shows until the person tries again.
  const [error, setError] = useState<unknown>(() => refusedFrom(params.get("refused")));

  // Each press begins a ceremony of its own, since the koko server's flow is taken by its first answer.
  const withPasskey = async () => {
    const ceremony = begin();
    if (ceremony === undefined) {
      return;
    }
    setPending("passkey");
    setError(undefined);
    try {
      const { flow, options } = await post(AuthPasskeySignInBegin, {});
      if (!ceremony.open()) {
        return;
      }
      const credential = await getPasskey(options, ceremony.signal);
      if (!ceremony.open()) {
        return;
      }
      const { user } = await post(AuthPasskeySignIn, { flow, credential });
      if (ceremony.open()) {
        navigate(nextPage(params.get("next")), { replace: true });
      } else {
        notify(t("passkey.signedIn", { name: user.name }));
      }
    } catch (failure) {
      if (ceremony.open()) {
        setError(cancelled(failure) ? undefined : failure);
        setPending(undefined);
      }
    }
  };

  // The begin carries next, for the callback to go on to once signed in; nothing goes there once Sign in closed.
  const withProvider = async () => {
    const page = begin();
    if (page === undefined) {
      return;
    }
    setPending("provider");
    setError(undefined);
    const next = params.get("next");
    try {
      await continueWithProvider(
        next === null ? { purpose: "signin" } : { purpose: "signin", next: nextPage(next) },
        page.open,
      );
    } catch (failure) {
      if (page.open()) {
        setError(failure);
        setPending(undefined);
      }
    }
  };

  const submit = async () => {
    setPending("password");
    setError(undefined);
    try {
      await post(AuthSignIn, { name, password });
      navigate(nextPage(params.get("next")), { replace: true });
    } catch (failure) {
      setError(failure);
      setPending(undefined);
    }
  };

  if (methods === undefined) {
    return (
      <Door title={t("signin.title")}>
        <Loading />
      </Door>
    );
  }
  return (
    <Door title={t("signin.title")}>
      {error !== undefined && (
        <div className="mb-4">
          <AlertLine>{refusalText(t, count, error)}</AlertLine>
        </div>
      )}
      {(methods.passkeys || methods.provider !== null) && (
        <>
          <div className="flex flex-col gap-3">
            {methods.passkeys && (
              <Button
                size="lg"
                fullWidth
                isPending={pending === "passkey"}
                onPress={() => void withPasskey()}
              >
                <FingerprintPattern aria-hidden className="size-5" />
                {t("passkey.signIn")}
              </Button>
            )}
            {methods.provider !== null && (
              <Button
                size="lg"
                fullWidth
                variant={methods.passkeys ? "secondary" : "primary"}
                isPending={pending === "provider"}
                onPress={() => void withProvider()}
              >
                <LogIn aria-hidden className="size-5" />
                {t("identity.continue", { name: methods.provider })}
              </Button>
            )}
          </div>
          <Separator className="my-6" />
        </>
      )}
      <Form label={t("signin.title")} onSubmit={() => void submit()} className="flex flex-col gap-4">
        <Field
          label={t("signin.name")}
          value={name}
          onChange={setName}
          autoComplete="username"
          code
          autoFocus
          validate={(v) => (v === "" ? t("signin.nameRequired") : undefined)}
        />
        <Field
          label={t("signin.password")}
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={setPassword}
          validate={(v) => (v === "" ? t("signin.passwordRequired") : undefined)}
        />
        <Button
          type="submit"
          size="lg"
          fullWidth
          variant={methods.passkeys || methods.provider !== null ? "secondary" : "primary"}
          isPending={pending === "password"}
          className="mt-2"
        >
          {t("signin.submit")}
        </Button>
        <p className="t-label">{t("signin.lost")}</p>
      </Form>
    </Door>
  );
}

/**
 * nextPage is where Sign in goes once signed in: the page next names, when
 * it is a page of this origin within the frame, else the first place (/).
 * A next of another origin, such as //example.com or https://example.com,
 * is never followed, nor one of the pages outside the frame.
 */
export function nextPage(next: string | null): string {
  if (next === null) {
    return "/";
  }
  try {
    const url = new URL(next, window.location.origin);
    const outside = [signInPage, linkPage, passwordPage].includes(url.pathname);
    if (url.origin === window.location.origin && next.startsWith("/") && !outside) {
      return url.pathname + url.search + url.hash;
    }
  } catch {
    // a next that does not parse names no page
  }
  return "/";
}
