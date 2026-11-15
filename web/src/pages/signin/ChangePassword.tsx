import { Button } from "@heroui/react";
import { useState } from "react";

import { AuthPassword, CodeWrongPassword } from "../../api/signin";
import { ApiError, post } from "../../client";
import { useCount, useT } from "../../shell/i18n";
import { Field } from "../../ui/fields";
import { Form } from "../../ui/Form";
import { notify } from "../../ui/notify";
import { AlertLine } from "../../ui/states";
import { PasswordFields } from "./PasswordFields";
import { refusalText } from "./refusal";

/**
 * ChangePassword changes the Password of the User this browser is signed
 * in as: the current one, then the new one twice, checked on submit, posted
 * to /auth/password, which keeps this Sign-in and ends the User's others
 * (onChanged hears it, so the Sign-ins are read again). For a User without
 * a Password (first), signed in another way, it sets a first one: the new
 * one twice alone, which ends no Sign-in. A refusal shows in place, the
 * form kept as typed; a wrong current Password says so, not
 * wrong_password's "User name or Password".
 */
export function ChangePassword({ user, first, onChanged }: { user: string; first: boolean; onChanged: () => void }) {
  const t = useT();
  const count = useCount();
  const [current, setCurrent] = useState("");
  const [value, setValue] = useState("");
  const [again, setAgain] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();
  const submitKey = first ? "signin.first.submit" : "signin.change.submit";

  const submit = async () => {
    setPending(true);
    setError(undefined);
    try {
      await post(AuthPassword, first ? { password: value } : { current, password: value });
      setCurrent("");
      setValue("");
      setAgain("");
      notify(t(first ? "signin.first.done" : "signin.change.done"));
      onChanged();
    } catch (failure) {
      setError(failure);
    } finally {
      setPending(false);
    }
  };

  return (
    <Form label={t(submitKey)} onSubmit={() => void submit()} className="flex flex-col gap-4">
      {/* The User name, for the browser's password manager to keep with the Password. */}
      <input type="text" name="username" autoComplete="username" value={user} readOnly hidden />
      {!first && (
        <Field
          label={t("signin.change.current")}
          type="password"
          autoComplete="current-password"
          value={current}
          onChange={setCurrent}
          validate={(v) => (v === "" ? t("signin.passwordRequired") : undefined)}
        />
      )}
      <PasswordFields user={user} value={value} again={again} onValue={setValue} onAgain={setAgain} />
      {error !== undefined && (
        <AlertLine>
          {error instanceof ApiError && error.code === CodeWrongPassword
            ? t("signin.change.wrong")
            : refusalText(t, count, error)}
        </AlertLine>
      )}
      <div>
        <Button type="submit" isPending={pending}>
          {t(submitKey)}
        </Button>
      </div>
    </Form>
  );
}
