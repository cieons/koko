import type { MessageKey } from "../../i18n/catalogues";
import type { Translate } from "../../shell/i18n";

// The names the record's pages show for its keys: each field of a Host or
// a Service, and each Preset Attribute. Any other Attribute key is shown as
// stored, as its value is.

/** named are the keys with a name of their own in the catalogues, host.key.<key>. */
const named = new Set([
  "id",
  "name",
  "space",
  "address",
  "port",
  "account",
  "relay",
  "state",
  "host_key",
  "host",
  "ports",
  "provider",
  "provider_account",
  "region",
  "price",
  "currency",
  "billing_cycle",
  "expires_at",
  "auto_renew",
]);

/** keyName is the name of a field or a Preset key in the language, or undefined for any other key. */
export function keyName(t: Translate, key: string): string | undefined {
  return named.has(key) ? t(`host.key.${key}` as MessageKey) : undefined;
}

/** keyLabel is a key as a header or a checkbox shows it: its name, or the key as stored. */
export function keyLabel(t: Translate, key: string): string {
  return keyName(t, key) ?? key;
}
