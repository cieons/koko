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
 * (onChanged hears it, so the Sign-ins are read again). A refusal shows in
 * place, the form kept as typed; a wrong current Password says so, not
 * wrong_password's "User name or Password".
 */
export function ChangePassword({ user, onChanged }: { user: string; onChanged: () => void }) {
  const t = useT();
  const count = useCount();
  const [current, setCurrent] = useState("");
  const [value, setValue] = useState("");
  const [again, setAgain] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();

  const submit = async () => {
    setPending(true);
    setError(undefined);
    try {
      await post(AuthPassword, { current, password: value });
      setCurrent("");
      setValue("");
      setAgain("");
      notify(t("signin.change.done"));
      onChanged();
    } catch (failure) {
      setError(failure);
    } finally {
      setPending(false);
    }
  };

  return (
    <Form label={t("signin.change.submit")} onSubmit={() => void submit()} className="flex flex-col gap-4">
      {/* The User name, for the browser's password manager to keep with the Password. */}
      <input type="text" name="username" autoComplete="username" value={user} readOnly hidden />
      <Field
        label={t("signin.change.current")}
        type="password"
        autoComplete="current-password"
        value={current}
        onChange={setCurrent}
        validate={(v) => (v === "" ? t("signin.passwordRequired") : undefined)}
      />
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
          {t("signin.change.submit")}
        </Button>
      </div>
    </Form>
  );
}
