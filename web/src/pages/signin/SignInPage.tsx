import { Button, Separator } from "@heroui/react";
import { FingerprintPattern } from "lucide-react";
import { useState } from "react";

import { AuthPasskeySignIn, AuthPasskeySignInBegin } from "../../api/passkey";
import { AuthSignIn } from "../../api/signin";
import { post } from "../../client";
import { useCount, useT } from "../../shell/i18n";
import { linkPage, navigate, passwordPage, signInPage, useSearchParams } from "../../shell/router";
import { Field } from "../../ui/fields";
import { Form } from "../../ui/Form";
import { AlertLine } from "../../ui/states";
import { Door } from "./Door";
import { refusalText } from "./refusal";
import { cancelled, getPasskey, passkeysAvailable, useCeremony } from "./webauthn";

/**
 * SignInPage is Sign in, outside the frame (docs/web-design.md, "Sign in
 * and a Sign-in Link's page"): Sign in with a passkey first, where the
 * browser has WebAuthn, then a divider and a User name and Password, posted
 * to /auth/signin. Either sets the Sign-in cookie; then the page next
 * names, on this origin only, or the first place. A refusal shows above
 * both, and too_many_attempts says in how many minutes to try again; a
 * passkey's ceremony the person cancelled shows nothing. One line says the
 * Owner or an Administrator can send a Sign-in Link, for a lost Password or
 * passkey.
 */
export function SignInPage() {
  const t = useT();
  const count = useCount();
  const [params] = useSearchParams();
  const begin = useCeremony();
  const passkeys = passkeysAvailable();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState<"password" | "passkey">();
  const [error, setError] = useState<unknown>();

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
      await post(AuthPasskeySignIn, { flow, credential });
      navigate(nextPage(params.get("next")), { replace: true });
    } catch (failure) {
      if (ceremony.open()) {
        setError(cancelled(failure) ? undefined : failure);
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

  return (
    <Door title={t("signin.title")}>
      {error !== undefined && (
        <div className="mb-4">
          <AlertLine>{refusalText(t, count, error)}</AlertLine>
        </div>
      )}
      {passkeys && (
        <>
          <Button
            size="lg"
            fullWidth
            isPending={pending === "passkey"}
            onPress={() => void withPasskey()}
          >
            <FingerprintPattern aria-hidden className="size-5" />
            {t("passkey.signIn")}
          </Button>
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
          variant={passkeys ? "secondary" : "primary"}
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
