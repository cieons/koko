import { Button } from "@heroui/react";
import { FileUp } from "lucide-react";
import { type Dispatch, type SetStateAction, useState } from "react";
import { FileTrigger } from "react-aria-components";

import { CodeAlreadyManaged, CodePassphraseNeeded, HostEnroll, type HostEnrollRequest } from "../../api/enroll";
import { CodeInvalidValue, type Host } from "../../api/host";
import { SecretList } from "../../api/secret";
import { ApiError } from "../../client";
import type { MessageKey } from "../../i18n/catalogues";
import { type Outcome, useRead, useWrite } from "../../shell/data";
import { differs } from "../../shell/form";
import { errorMessage, type Translate, useT } from "../../shell/i18n";
import { Field, SearchChoice, Segments } from "../../ui/fields";
import { FormDialog } from "../../ui/FormDialog";
import { notify } from "../../ui/notify";
import { LoadFailed } from "../../ui/states";
import { encodeValue } from "../secret/value";

// Enroll from the web UI (docs/web-design.md, "Enroll"): the koko server
// logs in to a Registered Host once with a password, a private key or a
// Secret, pins its host key and adds the koko key (ADR 0003). A password,
// a key and its passphrase are typed only in a secure context, never kept
// past the call that sends them, and never stored; a Secret crosses the API
// by its name alone.

/** EnrollWay is how Enroll logs in: not at all (Add Host's default), or with one of the three inputs. */
export type EnrollWay = "none" | "password" | "key" | "secret";

/** EnrollValues are the Enroll fields' values, as typed. */
export interface EnrollValues {
  way: EnrollWay;
  password: string;
  /** key is the private key, pasted or read from a file, in PEM. */
  key: string;
  /** passphrase opens key when it is encrypted. */
  passphrase: string;
  /**
   * askPassphrase asks for the passphrase whatever the key reads as: the
   * koko server answered passphrase_needed for a key the page did not read
   * as encrypted.
   */
  askPassphrase: boolean;
  /** secret is the Secret chosen, as <space>/<name>, or null. */
  secret: string | null;
  /** hostKey is the host key's fingerprint the Host must offer, or "" for any. */
  hostKey: string;
}

/** wayLabels are the segments' labels. */
const wayLabels: Readonly<Record<EnrollWay, MessageKey>> = {
  none: "host.enroll.way.none",
  password: "host.enroll.way.password",
  key: "host.enroll.way.key",
  secret: "host.enroll.way.secret",
};

/** maxInput is the longest password, key or passphrase Enroll takes, in bytes (api.EnrollInputMaxSize). */
const maxInput = 64 << 10;

/** enrollValues are the fields' values as they open: way chosen, nothing typed. */
export function enrollValues(way: EnrollWay): EnrollValues {
  return { way, password: "", key: "", passphrase: "", askPassphrase: false, secret: null, hostKey: "" };
}

/** withoutCredential are values with the password, the key and the passphrase cleared, as a form holds them once its call is sent. */
function withoutCredential(values: EnrollValues): EnrollValues {
  return { ...values, password: "", key: "", passphrase: "" };
}

/**
 * waysOffered are the ways to Enroll the fields offer, with none first when
 * Enroll is optional: a password and a private key only in a secure
 * context, where the page and what is typed in it reached the browser
 * encrypted, else a Secret alone.
 */
export function waysOffered(optional: boolean): EnrollWay[] {
  const ways: EnrollWay[] = window.isSecureContext ? ["password", "key", "secret"] : ["secret"];
  return optional ? ["none", ...ways] : ways;
}

/** fingerprintRule is a host key's fingerprint as record.CheckHostKey takes one: SHA256: and 43 characters of base64, its last two bits zero. */
const fingerprintRule = /^SHA256:[A-Za-z0-9+/]{42}[AEIMQUYcgkosw048]$/;

/** validFingerprint tells whether text is a host key's fingerprint, as koko host enroll --host-key checks it. */
export function validFingerprint(text: string): boolean {
  return fingerprintRule.test(text);
}

/** opensshMagic begins every private key in OpenSSH's format, once its base64 is decoded. */
const opensshMagic = "openssh-key-v1\0";

/**
 * encryptedKey tells whether pem is a private key encrypted in a form koko
 * decrypts: OpenSSH's, whose cipher is not "none", or a PEM block with an
 * ENCRYPTED Proc-Type header, indented or not, as Go's pem.Decode reads
 * it. Anything else, a key that does not parse among it, asks for no
 * passphrase, and the koko server says what is wrong; a passphrase_needed
 * it answers asks for one after all (askPassphrase).
 */
export function encryptedKey(pem: string): boolean {
  if (/^[ \t]*Proc-Type:[ \t]*4,ENCRYPTED[ \t]*\r?$/m.test(pem)) {
    return true;
  }
  const body = /-----BEGIN OPENSSH PRIVATE KEY-----([A-Za-z0-9+/=\s]*)-----END OPENSSH PRIVATE KEY-----/.exec(pem)?.[1];
  if (body === undefined) {
    return false;
  }
  let bytes: string;
  try {
    bytes = atob(body.replace(/\s+/g, ""));
  } catch {
    return false;
  }
  const at = opensshMagic.length;
  if (!bytes.startsWith(opensshMagic) || bytes.length < at + 4) {
    return false;
  }
  const length =
    ((bytes.charCodeAt(at) << 24) |
      (bytes.charCodeAt(at + 1) << 16) |
      (bytes.charCodeAt(at + 2) << 8) |
      bytes.charCodeAt(at + 3)) >>>
    0;
  return bytes.slice(at + 4, at + 4 + length) !== "none";
}

/** asksPassphrase tells whether the fields ask for the key's passphrase, and so whether it is sent. */
function asksPassphrase(values: EnrollValues): boolean {
  return values.way === "key" && (values.askPassphrase || encryptedKey(values.key));
}

/**
 * enrollRequest is host.enroll's request for the Host host names, by its
 * id, from values, whose way is not none: the password or the key as
 * typed, the passphrase when the fields ask for it, the Secret by its
 * Space and name, and the fingerprint when one is given.
 */
function enrollRequest(host: string, values: EnrollValues): HostEnrollRequest {
  const request: HostEnrollRequest = { host };
  switch (values.way) {
    case "password":
      request.password_base64 = encodeValue(values.password);
      break;
    case "key":
      request.private_key_base64 = encodeValue(values.key);
      if (asksPassphrase(values)) {
        request.passphrase_base64 = encodeValue(values.passphrase);
      }
      break;
    case "secret": {
      // A Space's name holds no '/', so the first one ends it.
      const secret = values.secret ?? "";
      const at = secret.indexOf("/");
      request.secret = { space: secret.slice(0, at), name: secret.slice(at + 1) };
      break;
    }
  }
  if (values.hostKey.trim() !== "") {
    request.host_key = values.hostKey.trim();
  }
  return request;
}

/**
 * enrollRefusal is what a form of the Enroll fields says of error: the
 * message of its code, but for invalid_value, which on Enroll means the
 * key or the Secret's value is not one it takes, rather than a field of
 * the Host form.
 */
export function enrollRefusal(t: Translate, error: unknown): string | undefined {
  if (error === undefined) {
    return undefined;
  }
  return error instanceof ApiError && error.code === CodeInvalidValue ? t("host.enroll.invalidValue") : errorMessage(t, error);
}

/** EnrollFieldsProps are what the Enroll fields are drawn from. */
export interface EnrollFieldsProps {
  values: EnrollValues;
  onChange: Dispatch<SetStateAction<EnrollValues>>;
  /** account is the Host Account Enroll logs in as, which names the password. */
  account: string;
  /** optional offers "Not now" first, as Add Host does. */
  optional?: boolean;
}

/**
 * EnrollFields are the ways to Enroll as segments, then the field of the
 * way chosen: a password; a private key, pasted or read from a file in the
 * browser, with its passphrase once the key shows it is encrypted, or the
 * koko server said it is; or a Secret the caller can use; and, for any of
 * them, the host key's fingerprint. Outside a secure context they offer a
 * Secret alone, and say why.
 */
export function EnrollFields({ values, onChange, account, optional = false }: EnrollFieldsProps) {
  const t = useT();
  const ways = waysOffered(optional);
  const secrets = useRead(SecretList, {}, { enabled: values.way === "secret" });
  const set =
    <K extends keyof EnrollValues>(key: K) =>
    (value: EnrollValues[K]) =>
      onChange((v) => ({ ...v, [key]: value }));
  const required = (v: string) => (v === "" ? t("host.form.required") : undefined);
  const notTooLong = (v: string) =>
    new TextEncoder().encode(v).length > maxInput ? t("host.enroll.tooLong") : required(v);
  const [fileProblem, setFileProblem] = useState<MessageKey>();
  const readFile = (files: FileList | null) => {
    const file = files?.[0];
    setFileProblem(undefined);
    if (file === undefined) {
      return;
    }
    // A file too long for a key is refused before it is read, so a large one picked by mistake is never loaded.
    if (file.size > maxInput) {
      setFileProblem("host.enroll.tooLong");
      return;
    }
    file.text().then(
      // The key goes in only while the private key is still the way chosen, beside what changed meanwhile.
      (key) => onChange((v) => (v.way === "key" ? { ...v, key, passphrase: "" } : v)),
      () => setFileProblem("host.enroll.keyUnreadable"),
    );
  };
  return (
    <>
      {ways.length > 1 && (
        <Segments
          label={t("host.enroll.way")}
          value={values.way}
          onChange={(way) => onChange((v) => ({ ...withoutCredential(v), askPassphrase: false, way: way as EnrollWay }))}
          options={ways.map((way) => ({ id: way, label: t(wayLabels[way]) }))}
        />
      )}
      {!window.isSecureContext && <p className="t-label">{t("host.enroll.insecure")}</p>}
      {values.way === "password" && (
        <Field
          label={t("host.enroll.password", { account })}
          type="password"
          value={values.password}
          onChange={set("password")}
          validate={notTooLong}
          credential
        />
      )}
      {values.way === "key" && (
        <div className="flex flex-col gap-2">
          <Field
            label={t("host.enroll.key")}
            value={values.key}
            onChange={set("key")}
            validate={notTooLong}
            multiline
            rows={6}
            code
            credential
          />
          <FileTrigger onSelect={readFile}>
            <Button variant="tertiary" size="sm" className="self-start">
              <FileUp aria-hidden className="size-4" />
              {t("host.enroll.keyFile")}
            </Button>
          </FileTrigger>
          {fileProblem !== undefined && <p className="text-sm text-danger">{t(fileProblem)}</p>}
        </div>
      )}
      {asksPassphrase(values) && (
        <Field
          label={t("host.enroll.passphrase")}
          type="password"
          value={values.passphrase}
          onChange={set("passphrase")}
          validate={notTooLong}
          credential
        />
      )}
      {values.way === "secret" && (
        <>
          <SearchChoice
            label={t("host.enroll.secret")}
            value={values.secret}
            onChange={set("secret")}
            options={(secrets.data?.secrets ?? []).map((s) => {
              const name = `${s.space}/${s.name}`;
              return { id: name, label: name };
            })}
            validate={(v) => (v === null ? t("host.enroll.secretRequired") : undefined)}
          />
          {secrets.error !== undefined && <LoadFailed error={secrets.error} retry={secrets.reload} />}
        </>
      )}
      {values.way !== "none" && (
        <Field
          label={t("host.enroll.hostKey")}
          value={values.hostKey}
          onChange={set("hostKey")}
          description={t("host.enroll.hostKeyHint")}
          placeholder="SHA256:…"
          validate={(v) => (v.trim() === "" || validFingerprint(v.trim()) ? undefined : t("host.enroll.hostKeyRule"))}
          code
        />
      )}
    </>
  );
}

/**
 * useEnroll is host.enroll for a form of the Enroll fields, which holds
 * their values with setValues. send builds the request for the Host whose
 * id is host from values, clears the credential from the form as the call
 * is sent, and answers the outcome; the hook keeps the request only as a
 * salted digest (useWrite's credential), so the same credential given again
 * reuses the Idempotency-Key. A passphrase_needed answer makes the fields
 * ask for the passphrase.
 */
export function useEnroll(setValues: Dispatch<SetStateAction<EnrollValues>>) {
  const write = useWrite(HostEnroll, { credential: true });
  const send = async (host: string, values: EnrollValues): Promise<Outcome<{ host: Host }>> => {
    const request = enrollRequest(host, values);
    setValues(withoutCredential);
    const outcome = await write.send(request);
    if (outcome.error instanceof ApiError && outcome.error.code === CodePassphraseNeeded) {
      setValues((v) => ({ ...v, askPassphrase: true }));
    }
    return outcome;
  };
  return { send, pending: write.pending, error: write.error };
}

/** EnrollHostProps are what Enroll on a Host's page acts on. */
export interface EnrollHostProps {
  host: Host;
  /** onEnrolled hears that the Host is Managed now. */
  onEnrolled: () => void;
  /** onStale hears that the Host is Managed already, which the page beneath may not show yet. */
  onStale: () => void;
  onClose: () => void;
}

/**
 * EnrollHost is Enroll on a Registered Host's page: a short form of the
 * Enroll fields, which sends one host.enroll naming the Host by its id and
 * clears the credential as it sends it; a refusal shows in place, the
 * credential to be given again, and already_managed loads the page again.
 * Once the Host is Managed, a toast names the host key pinned, as koko
 * host enroll prints it.
 */
export function EnrollHost({ host, onEnrolled, onStale, onClose }: EnrollHostProps) {
  const t = useT();
  const [opened] = useState(() => enrollValues(waysOffered(false)[0] ?? "secret"));
  const [values, setValues] = useState(opened);
  const enroll = useEnroll(setValues);
  const submit = () => {
    void enroll.send(host.id, values).then(({ response, error }) => {
      if (response !== undefined) {
        notify(t("host.enroll.done", { name: response.host.name, fingerprint: response.host.host_key ?? "" }));
        onEnrolled();
      } else if (error instanceof ApiError && error.code === CodeAlreadyManaged) {
        onStale();
      }
    });
  };
  return (
    <FormDialog
      title={t("host.enroll.title", { name: host.name })}
      submit={t("host.enroll")}
      pending={enroll.pending}
      changed={differs(values, opened)}
      error={enrollRefusal(t, enroll.error)}
      onSubmit={submit}
      onClose={onClose}
      size="md"
    >
      <EnrollFields values={values} onChange={setValues} account={host.account} />
    </FormDialog>
  );
}
