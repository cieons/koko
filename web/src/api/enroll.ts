// Code generated from internal/shared/api/enroll.go by make generate; DO NOT EDIT.

import type { Op } from "./api";
import type { Host } from "./host";

/**
 * HostEnrollRequest Enrolls a Host: the koko server logs in to it once as
 * its Host Account with the password or the private key given, or held in
 * the Secret named, never with the koko key, checks its host key, and
 * appends the koko key to the Host Account's authorized_keys.
 */
export interface HostEnrollRequest {
  /** Host names the Host, by its name or its id. */
  host: string;
  /**
   * Password is the Host Account's password. Exactly one of Password,
   * PrivateKey and Secret is given.
   */
  password_base64?: string;
  /**
   * PrivateKey is a private key the Host Account accepts, unencrypted, in
   * a form golang.org/x/crypto/ssh parses: OpenSSH, PKCS #1, PKCS #8 or
   * SEC 1, in PEM. koko host enroll --key decrypts an encrypted key on the
   * device, so its passphrase never leaves it.
   */
  private_key_base64?: string;
  /**
   * Secret names a Secret that holds the Host Account's password or a
   * private key it accepts. The koko server reads it on its own side, for
   * the caller, who must be able to act in its Space, so the value never
   * crosses the API. A value that begins with a PEM header ("-----BEGIN ")
   * is a private key, taken as PrivateKey is; any other is a password, as
   * EnrollPassword reads one.
   */
  secret?: SecretRef;
  /**
   * HostKey, when given, is the SHA-256 fingerprint ("SHA256:...") of the
   * only host key Enroll accepts, which it then pins in place of any pin
   * the Host has.
   */
  host_key?: string;
  /**
   * ReplaceHostKey pins whatever host key the Host offers, in place of
   * the pinned one, with the login as its only proof: a fresh trust on
   * first use. It is not given with HostKey.
   */
  replace_host_key?: boolean;
}

/**
 * SecretRef names a Secret, by its Space and its name, as the secret
 * operations name one.
 */
export interface SecretRef {
  space: string;
  name: string;
}

/**
 * HostEnrollResponse is the Host Enroll left Managed, with its pinned host
 * key.
 */
export interface HostEnrollResponse {
  host: Host;
}

/**
 * HostEnroll makes a Registered Host Managed: it pins the host key the Host
 * offers and appends the koko key to its Host Account's authorized_keys,
 * changing nothing else there. A Host with a pin must offer that key;
 * host_key or replace_host_key pins a new one, which is also the only way to
 * Enroll a Managed Host again. A Secret is read as secret.show reads it, so
 * one in a Space the caller cannot act in is refused as one that does not
 * exist, with CodeUnknownSpace, whatever the caller can do on the Host. The
 * login runs before the write's transaction, so a slow Host holds up no
 * other write.
 */
export const HostEnroll: Op<HostEnrollRequest, HostEnrollResponse> = {
  name: "host.enroll",
  path: "/v1/host.enroll",
  write: true,
};

/**
 * CodeUnexpectedHostKey: the Host offered a host key other than the
 * pinned one, or the one host_key names, so koko did not log in; the
 * message holds both fingerprints and the two ways on, host_key and
 * replace_host_key.
 */
export const CodeUnexpectedHostKey = "unexpected_host_key";

/**
 * CodeLoginRefused: the Host refused the password or the private key
 * for its Host Account.
 */
export const CodeLoginRefused = "login_refused";

/**
 * CodeHostUnreachable: the koko server could not connect to the Host,
 * after retrying the network errors.
 */
export const CodeHostUnreachable = "host_unreachable";

/**
 * CodeKeyNotAdded: koko logged in to the Host but could not append the
 * koko key to the Host Account's authorized_keys, such as for an
 * account without a shell; the Host stays as it was in koko's record.
 */
export const CodeKeyNotAdded = "key_not_added";

/**
 * CodeAlreadyManaged: the Host is Managed, and the call gives neither
 * host_key nor replace_host_key, which alone Enroll a Managed Host
 * again.
 */
export const CodeAlreadyManaged = "already_managed";

/**
 * CodeHostChanged: the Host's Host Account, state or pinned host key
 * changed while Enroll logged in to it, so Enroll recorded nothing,
 * though the koko key may be in the authorized_keys of the Host Account
 * it logged in as; it may run again.
 */
export const CodeHostChanged = "host_changed";
