import { CodeTooManyAttempts } from "../../api/signin";
import { ApiError } from "../../client";
import { type Count, errorMessage, type Translate } from "../../shell/i18n";

/**
 * refusalText says why signing in failed: for too_many_attempts, in how
 * many whole minutes to try again, from its Retry-After; else the code's
 * message.
 */
export function refusalText(t: Translate, count: Count, error: unknown): string {
  if (error instanceof ApiError && error.code === CodeTooManyAttempts && error.retryAfter !== undefined) {
    return count("signin.tooMany", Math.max(1, Math.ceil(error.retryAfter / 60)));
  }
  return errorMessage(t, error);
}
