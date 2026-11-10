import { Button } from "@heroui/react";
import { useState } from "react";

import { AuthSignIn } from "../../api/signin";
import { post } from "../../client";
import { useCount, useT } from "../../shell/i18n";
import { navigate, passwordPage, signInPage, useSearchParams } from "../../shell/router";
import { Field } from "../../ui/fields";
import { Form } from "../../ui/Form";
import { AlertLine } from "../../ui/states";
import { Door } from "./Door";
import { refusalText } from "./refusal";

/**
 * SignInPage is Sign in, outside the frame (docs/web-design.md, "Sign in
 * and Set a Password"): a User name and Password, posted to /auth/signin,
 * which sets the Sign-in cookie; then the page next names, on this origin
 * only, or the first place. A refusal shows above the fields, and
 * too_many_attempts says in how many minutes to try again. A lost Password
 * is reset by the Owner, which one line says.
 */
export function SignInPage() {
  const t = useT();
  const count = useCount();
  const [params] = useSearchParams();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();

  const submit = async () => {
    setPending(true);
    setError(undefined);
    try {
      await post(AuthSignIn, { name, password });
      navigate(nextPage(params.get("next")), { replace: true });
    } catch (failure) {
      setError(failure);
      setPending(false);
    }
  };

  return (
    <Door title={t("signin.title")} lead={t("signin.lead")}>
      <Form label={t("signin.title")} onSubmit={() => void submit()} className="flex flex-col gap-4">
        {error !== undefined && <AlertLine>{refusalText(t, count, error)}</AlertLine>}
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
        <Button type="submit" size="lg" fullWidth isPending={pending} className="mt-2">
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
    const outside = url.pathname === signInPage || url.pathname === passwordPage;
    if (url.origin === window.location.origin && next.startsWith("/") && !outside) {
      return url.pathname + url.search + url.hash;
    }
  } catch {
    // a next that does not parse names no page
  }
  return "/";
}
