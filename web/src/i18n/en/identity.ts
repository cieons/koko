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
  "identity.name.hint": "Shown on the sign-in button, such as Authentik.",
  "identity.name.required": "Enter the name.",
  "identity.name.tooLong": "The name must be at most 40 characters long.",
  "identity.redirect": "Redirect URI",
  "identity.redirect.hint": "Register this redirect URI at the provider.",
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
  "identity.issuerChange.confirm": "Change issuer",
  "identity.remove": "Remove",
  "identity.remove.title": "Remove {name}",
  "identity.remove.consequence.one":
    "The {count} Linked Identity is unlinked, and the browsers signed in through it are signed out.",
  "identity.remove.consequence.other":
    "The {count} Linked Identities are unlinked, and the browsers signed in through them are signed out.",
  "identity.removed": "{name} removed.",
  "error.invalid_provider":
    "A setting of the Identity Provider is not valid, or its issuer's discovery document is not.",
  "error.provider_unreachable": "The Identity Provider did not answer in time. Try again later.",
  "error.provider_unset": "No Identity Provider is configured.",
};
