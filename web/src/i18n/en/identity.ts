// The identity area's messages: the Identity provider panel over Access,
// and its API error codes.
export default {
  "identity.provider": "Identity provider",
  "identity.issuer": "Issuer URL",
  "identity.issuer.hint": "As the provider's discovery document names it, a trailing / included.",
  "identity.issuer.required": "Enter the issuer URL.",
  "identity.issuer.https": "The issuer URL must start with https://.",
  "identity.clientID": "Client ID",
  "identity.clientID.required": "Enter the client ID.",
  "identity.secret": "Client secret",
  "identity.secret.set": "Set. Leave the field empty to keep it.",
  "identity.secret.isSet": "Set",
  "identity.secret.required": "Enter the client secret.",
  "identity.secret.checked": "The client ID and the client secret are checked at the first sign-in through the provider.",
  "identity.name": "Name",
  "identity.name.required": "Enter the name.",
  "identity.name.tooLong": "The name must be at most 40 characters long.",
  "identity.redirect": "Redirect URI",
  "identity.redirect.copy": "Copy the redirect URI",
  "identity.webURLUnset":
    "The redirect URI is under the Web URL, which is not set. Set it on the koko server's machine with koko server set web_url=.",
  "identity.none": "No identity provider is configured.",
  "identity.save": "Save",
  "identity.saved": "{name} saved.",
  "identity.savedUnlinked.one": "{name} saved. {count} Linked Identity was unlinked.",
  "identity.savedUnlinked.other": "{name} saved. {count} Linked Identities were unlinked.",
  "identity.issuerChange.title": "Change the issuer of {name}",
  "identity.issuerChange.consequence.one":
    "The {count} Linked Identity at the current issuer is unlinked, and the browsers signed in through it are signed out. Its User links an identity again at the new issuer.",
  "identity.issuerChange.consequence.other":
    "The {count} Linked Identities at the current issuer are unlinked, and the browsers signed in through them are signed out. Each User links an identity again at the new issuer.",
  "identity.issuerChange.none": "No Linked Identity is at the current issuer, so none is unlinked.",
  "identity.issuerChange.confirm": "Change issuer",
  "identity.remove": "Remove",
  "identity.remove.title": "Remove {name}",
  "identity.remove.consequence.one":
    "The {count} Linked Identity is unlinked, and the browsers signed in through it are signed out.",
  "identity.remove.consequence.other":
    "The {count} Linked Identities are unlinked, and the browsers signed in through them are signed out.",
  "identity.remove.none": "No Linked Identity is at the provider, so none is unlinked.",
  "identity.removed": "{name} removed.",
  "identity.reason.issuer_invalid":
    "The issuer URL is not valid: it is an https:// URL without a query, a fragment or spaces, at most 512 bytes long.",
  "identity.reason.client_id_invalid": "The client ID is not valid: 1 to 255 bytes, without control characters.",
  "identity.reason.client_secret_invalid":
    "The client secret is not valid: 1 to 1,024 bytes, without control characters such as a line ending.",
  "identity.reason.name_invalid":
    "The name is not valid: 1 to 40 characters, without control characters or spaces at either end.",
  "identity.reason.no_discovery": "No discovery document answered at the issuer URL. Check the issuer URL.",
  "identity.reason.discovery_unreadable":
    "The issuer's discovery document could not be read: it is not JSON, is larger than 1 MiB, or was redirected more than 3 times or to http://.",
  "identity.reason.issuer_mismatch":
    "The discovery document names a different issuer. The issuer URL must match it exactly, a trailing / included.",
  "identity.reason.endpoint_missing":
    "The discovery document gives no https:// authorization, token or key set endpoint.",
  "identity.reason.code_unsupported": "The provider does not offer the authorization code flow koko signs in with.",
  "error.invalid_provider":
    "A setting of the Identity Provider is not valid, or its issuer's discovery document is not.",
  "error.provider_unreachable": "The Identity Provider did not answer in time. Try again later.",
  "error.provider_unset": "No Identity Provider is configured.",
  "error.provider_refused": "The Identity Provider did not complete the sign-in. Try again, or sign in another way.",
  "error.provider_failed": "The Identity Provider's answer could not be verified. Try again, or sign in another way.",
  "error.identity_not_linked": "No User has this identity linked. Sign in another way, then link it on Account.",
  "error.identity_linked": "A Linked Identity is already linked. Unlink it on Account before linking another.",
  "error.identity_taken": "Another User has already linked this identity.",
  "error.signin_expired":
    "The sign-in through the Identity Provider expired or was started in another browser. Start it again.",
};
