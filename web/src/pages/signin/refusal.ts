import { CodeTooManyAttempts } from "../../api/signin";
import { ApiError } from "../../client";
import { type Count, errorMessage, type Translate } from "../../shell/i18n";
import { NoCredential } from "./webauthn";

/**
 * refusalText says why signing in, or making a passkey, failed: for
 * too_many_attempts, in how many whole minutes to try again, from its
 * Retry-After; for a passkey's ceremony the browser ended, that the device
 * holds one of the User's passkeys already (InvalidStateError) or that the
 * browser could not complete it; else the code's message. A ceremony the
 * person cancelled shows nothing, so never comes here.
 */
export function refusalText(t: Translate, count: Count, error: unknown): string {
  if (error instanceof ApiError && error.code === CodeTooManyAttempts && error.retryAfter !== undefined) {
    return count("signin.tooMany", Math.max(1, Math.ceil(error.retryAfter / 60)));
  }
  if (error instanceof DOMException && error.name === "InvalidStateError") {
    return t("passkey.exists");
  }
  if (error instanceof DOMException || error instanceof NoCredential) {
    return t("passkey.failed");
  }
  return errorMessage(t, error);
}
