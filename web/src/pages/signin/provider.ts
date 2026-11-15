import { AuthOIDCBegin, type AuthOIDCBeginRequest } from "../../api/identity";
import { ApiError, post } from "../../client";
import { leaveFor } from "../../shell/leave";

/**
 * refusedCodes are the codes the Identity Provider's callback sends a
 * browser back with, as ?refused=<code> on Sign in, Account or a Sign-in
 * Link's page (docs/subsystems/identity.md). Each has its message.
 */
const refusedCodes: ReadonlySet<string> = new Set([
  "signin_expired",
  "provider_refused",
  "provider_failed",
  "provider_unreachable",
  "identity_not_linked",
  "identity_linked",
  "identity_taken",
  "signin_required",
  "link_used",
  "link_invalid",
  "too_many_attempts",
  "internal_error",
]);

/**
 * refusedFrom is the refusal a page's ?refused=<code> names, as the
 * callback's own refusal would be, or undefined for none or a code the
 * callback never sends, which the page then ignores.
 */
export function refusedFrom(code: string | null): ApiError | undefined {
  if (code === null || !refusedCodes.has(code)) {
    return undefined;
  }
  return new ApiError(code, `the Identity Provider's callback refused with ${code}`);
}

/**
 * continueWithProvider begins a flow through the Identity Provider, to sign
 * in or to link the User's identity, and sends the browser to the
 * provider, only while open says the page that began it is still open: a
 * begin answered after it closed goes nowhere. A refusal throws.
 */
export async function continueWithProvider(request: AuthOIDCBeginRequest, open: () => boolean): Promise<void> {
  const { location } = await post(AuthOIDCBegin, request);
  if (open()) {
    leaveFor(location);
  }
}
