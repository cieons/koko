import { useCount, useT } from "../../shell/i18n";
import { Field } from "../../ui/fields";

/** The Password rule's bounds, in Unicode code points after NFC normalisation (api.MinPasswordLength, api.MaxPasswordLength). */
export const minPasswordLength = 15;
export const maxPasswordLength = 128;

/** passwordLength is a Password's length as the koko server counts it: code points after NFC normalisation. */
export function passwordLength(password: string): number {
  return [...password.normalize("NFC")].length;
}

/** PasswordFieldsProps are the two fields of a new Password and whose it is. */
export interface PasswordFieldsProps {
  /** user is the User the Password is for, whose name it must not be. */
  user: string;
  value: string;
  again: string;
  onValue: (value: string) => void;
  onAgain: (value: string) => void;
}

/**
 * PasswordFields asks for a new Password twice, as Set a Password and
 * Account's Password panel do: the first with a count toward 15
 * characters under it, the second to compare. Each field holds the rule's
 * message, shown when its form is submitted: 15 to 128 characters, not the
 * User's name, and the two the same. The koko server checks the rule again.
 */
export function PasswordFields({ user, value, again, onValue, onAgain }: PasswordFieldsProps) {
  const t = useT();
  const count = useCount();
  const length = passwordLength(value);
  const help =
    length === 0
      ? t("signin.password.rule")
      : length < minPasswordLength
        ? count("signin.password.count", length)
        : length > maxPasswordLength
          ? t("signin.password.over", { count: length })
          : t("signin.password.long");
  return (
    <>
      <Field
        label={t("signin.password.new")}
        type="password"
        autoComplete="new-password"
        value={value}
        onChange={onValue}
        description={help}
        validate={(v) =>
          passwordLength(v) < minPasswordLength
            ? t("signin.password.tooShort")
            : passwordLength(v) > maxPasswordLength
              ? t("signin.password.tooLong")
              : v.normalize("NFC") === user
                ? t("signin.password.notName")
                : undefined
        }
      />
      <Field
        label={t("signin.password.again")}
        type="password"
        autoComplete="new-password"
        value={again}
        onChange={onAgain}
        validate={(v) => (v !== value ? t("signin.password.mismatch") : undefined)}
      />
    </>
  );
}
