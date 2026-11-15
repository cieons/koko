import { Button } from "@heroui/react";
import { type ReactNode, useEffect, useRef, useState } from "react";

import {
  type IdentityProvider,
  ServerOIDCRemove,
  ServerOIDCSet,
  type ServerOIDCSetRequest,
  ServerOIDCShow,
  type ServerOIDCShowResponse,
} from "../../api/identity";
import { useCaller } from "../../shell/caller";
import { type Read, useRead, useWrite } from "../../shell/data";
import { ApiError } from "../../client";
import { catalogues, type MessageKey } from "../../i18n/catalogues";
import { errorMessage, type Translate, useCount, useT } from "../../shell/i18n";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { CopyButton } from "../../ui/copy";
import { Field } from "../../ui/fields";
import { notify, notifyRefusal } from "../../ui/notify";
import { SidePanel } from "../../ui/SidePanel";
import { Loaded, Refusal } from "../../ui/states";
import { Fact } from "../host/Fact";

/** maxName is the longest name on the sign-in button, in characters (api.MaxProviderNameLength). */
const maxName = 40;

/** ProviderPanelProps are what closes the Identity provider panel. */
export interface ProviderPanelProps {
  onClose: () => void;
}

/**
 * ProviderPanel is the Identity provider panel over Access
 * (docs/web-design.md, "Access"), from server.oidc.show: for the Owner, the
 * form that sets it (ProviderForm); for an Administrator, the same read
 * only. The client secret is never shown.
 */
export function ProviderPanel({ onClose }: ProviderPanelProps) {
  const caller = useCaller();
  const read = useRead(ServerOIDCShow, {});
  return caller.owner ? <ProviderForm read={read} onClose={onClose} /> : <ProviderFacts read={read} onClose={onClose} />;
}

/** Values are the form's settings but the secret, as typed. */
interface Values {
  issuer: string;
  clientID: string;
  name: string;
}

/** valuesOf are the settings of p as the form opens with them, or empty fields for none. */
function valuesOf(p: IdentityProvider | null): Values {
  return { issuer: p?.issuer ?? "", clientID: p?.client_id ?? "", name: p?.name ?? "" };
}

/**
 * ProviderForm is the Owner's form: Issuer URL, Client ID and Name; Client
 * secret, an empty field that keeps the stored secret when left empty; and
 * the redirect URI with Copy. Save sends only what changed, asking first
 * when the issuer changes, naming how many Linked Identities it unlinks,
 * and shows a refusal in place; Remove asks first. The secret is dropped as
 * it is sent, and with the form. Without a Web URL, which the redirect URI
 * is under, the form is disabled, saying why.
 */
function ProviderForm({ read, onClose }: { read: Read<ServerOIDCShowResponse>; onClose: () => void }) {
  const t = useT();
  const count = useCount();
  // The form opens with the first answer, and keeps what is typed while
  // the page reads it again; undefined until it comes.
  const [stored, setStored] = useState<IdentityProvider | null>();
  const [values, setValues] = useState<Values>(valuesOf(null));
  if (stored === undefined && read.data !== undefined) {
    setStored(read.data.provider);
    setValues(valuesOf(read.data.provider));
  }
  const [secret, setSecret] = useState("");
  const [confirm, setConfirm] = useState<"issuer" | "remove">();
  const set = useWrite(ServerOIDCSet, { credential: true });
  const remove = useWrite(ServerOIDCRemove);
  // A call that answers after the panel closed tells its outcome in a toast.
  const closed = useRef(false);
  useEffect(() => {
    closed.current = false;
    return () => {
      closed.current = true;
    };
  }, []);
  const redirect = read.data?.redirect_uri ?? null;
  const disabled = redirect === null;
  const opened = valuesOf(stored ?? null);
  const changed =
    values.issuer !== opened.issuer || values.clientID !== opened.clientID || values.name !== opened.name || secret !== "";
  const field = (key: keyof Values) => (value: string) => setValues((v) => ({ ...v, [key]: value }));

  const request = (): ServerOIDCSetRequest => ({
    ...(values.issuer !== opened.issuer ? { issuer: values.issuer } : {}),
    ...(values.clientID !== opened.clientID ? { client_id: values.clientID } : {}),
    ...(values.name !== opened.name ? { name: values.name } : {}),
    ...(secret !== "" ? { client_secret: secret } : {}),
  });
  const save = async () => {
    // The secret stays in its field until it is saved, so a retry after a
    // refusal sends it again; the panel drops it as it closes.
    const { response, error } = await set.send(request());
    if (closed.current) {
      if (response === undefined) {
        notifyRefusal(refusalMessage(t, error));
      } else {
        notify(saved(response.provider.name, response.identities_unlinked));
      }
      return;
    }
    setConfirm(undefined);
    if (response !== undefined) {
      setSecret("");
      setStored(response.provider);
      setValues(valuesOf(response.provider));
      notify(saved(response.provider.name, response.identities_unlinked));
      read.reload();
    }
  };
  const saved = (name: string, unlinked: number) =>
    unlinked === 0 ? t("identity.saved", { name }) : count("identity.savedUnlinked", unlinked, { name });
  // linked is how many Linked Identities a change of issuer or a removal
  // unlinks, as the page last read it; asking first reads it again.
  const linked = read.data?.provider?.linked_identities ?? stored?.linked_identities ?? 0;
  const ask = (question: "issuer" | "remove") => {
    read.reload();
    setConfirm(question);
  };
  const submit = () => {
    if (stored === undefined || disabled || !changed) {
      return;
    }
    if (stored !== null && values.issuer !== stored.issuer) {
      ask("issuer");
    } else {
      void save();
    }
  };
  const removeProvider = async () => {
    const { response } = await remove.send({});
    if (response !== undefined && stored) {
      notify(t("identity.removed", { name: stored.name }));
      onClose();
    }
  };

  const fields = (
    <>
      {disabled && <p className="t-label">{t("identity.webURLUnset")}</p>}
      <Field
        label={t("identity.issuer")}
        value={values.issuer}
        onChange={field("issuer")}
        description={t("identity.issuer.hint")}
        type="url"
        code
        isDisabled={disabled}
        autoFocus={!disabled}
        validate={(v) =>
          v === "" ? t("identity.issuer.required") : v.startsWith("https://") ? undefined : t("identity.issuer.https")
        }
      />
      <Field
        label={t("identity.clientID")}
        value={values.clientID}
        onChange={field("clientID")}
        code
        isDisabled={disabled}
        validate={(v) => (v === "" ? t("identity.clientID.required") : undefined)}
      />
      <Field
        label={t("identity.secret")}
        value={secret}
        onChange={setSecret}
        type="password"
        credential
        description={stored?.client_secret_set ? t("identity.secret.set") : undefined}
        isDisabled={disabled}
        validate={(v) => (v === "" && stored === null ? t("identity.secret.required") : undefined)}
      />
      <Field
        label={t("identity.name")}
        value={values.name}
        onChange={field("name")}
        isDisabled={disabled}
        validate={(v) =>
          v.trim() === "" ? t("identity.name.required") : [...v].length > maxName ? t("identity.name.tooLong") : undefined
        }
      />
      {redirect !== null && <RedirectURI uri={redirect} />}
      {!disabled && <p className="t-label">{t("identity.secret.checked")}</p>}
      <Refusal error={set.error === undefined ? undefined : refusalMessage(t, set.error)} />
    </>
  );
  const footer = (
    <>
      {stored && (
        <Button variant="danger-soft" className="mr-auto" onPress={() => ask("remove")}>
          {t("identity.remove")}
        </Button>
      )}
      <Button variant="tertiary" onPress={onClose}>
        {t("shell.cancel")}
      </Button>
      <Button type="submit" isPending={set.pending} isDisabled={disabled || !changed}>
        {t("identity.save")}
      </Button>
    </>
  );
  return (
    <SidePanel
      title={t("identity.provider")}
      subtitle={stored?.name}
      onSubmit={submit}
      changed={changed}
      pending={set.pending || remove.pending}
      onClose={onClose}
      footer={stored === undefined ? undefined : footer}
    >
      <Loaded read={read}>{() => fields}</Loaded>
      {confirm === "issuer" && stored && (
        <ConfirmDialog
          title={t("identity.issuerChange.title", { name: stored.name })}
          consequence={
            linked === 0 ? t("identity.issuerChange.none") : count("identity.issuerChange.consequence", linked)
          }
          confirm={t("identity.issuerChange.confirm")}
          pending={set.pending}
          onConfirm={() => void save()}
          onCancel={() => setConfirm(undefined)}
        />
      )}
      {confirm === "remove" && stored && (
        <ConfirmDialog
          title={t("identity.remove.title", { name: stored.name })}
          consequence={linked === 0 ? t("identity.remove.none") : count("identity.remove.consequence", linked)}
          confirm={t("identity.remove")}
          pending={remove.pending}
          error={remove.error}
          onConfirm={() => void removeProvider()}
          onCancel={() => setConfirm(undefined)}
        />
      )}
    </SidePanel>
  );
}

/**
 * ProviderFacts is the panel an Administrator sees: the provider's settings
 * and the redirect URI, without any action, or that none is configured.
 */
function ProviderFacts({ read, onClose }: { read: Read<ServerOIDCShowResponse>; onClose: () => void }) {
  const t = useT();
  return (
    <SidePanel title={t("identity.provider")} subtitle={read.data?.provider?.name} onClose={onClose}>
      <Loaded read={read}>{(shown) => <Facts shown={shown} />}</Loaded>
    </SidePanel>
  );
}

/** Facts are the provider's settings and the redirect URI, as an Administrator sees them. */
function Facts({ shown }: { shown: ServerOIDCShowResponse }) {
  const t = useT();
  const p = shown.provider;
  return (
    <>
      {p === null ? (
        <p className="text-muted">{t("identity.none")}</p>
      ) : (
        <dl className="flex flex-col gap-4">
          <Fact label={t("identity.issuer")}>
            <span className="t-code">{p.issuer}</span>
          </Fact>
          <Fact label={t("identity.clientID")}>
            <span className="t-code">{p.client_id}</span>
          </Fact>
          <Fact label={t("identity.secret")}>{t("identity.secret.isSet")}</Fact>
          <Fact label={t("identity.name")}>{p.name}</Fact>
        </dl>
      )}
      {shown.redirect_uri === null ? (
        <p className="t-label">{t("identity.webURLUnset")}</p>
      ) : (
        <RedirectURI uri={shown.redirect_uri} />
      )}
    </>
  );
}

/** RedirectURI is the redirect URI to register at the provider, with Copy. */
function RedirectURI({ uri }: { uri: string }): ReactNode {
  const t = useT();
  return (
    <section className="flex flex-col gap-2">
      <h3 className="t-label">{t("identity.redirect")}</h3>
      <div className="flex items-center gap-3">
        <span className="t-code min-w-0 flex-1 break-all">{uri}</span>
        <CopyButton value={uri} label={t("identity.redirect.copy")} size="sm" />
      </div>
    </section>
  );
}

/**
 * refusalMessage is how the panel words a refused save: for
 * invalid_provider, the check its reason names, in the reader's language;
 * otherwise its code's message.
 */
function refusalMessage(t: Translate, error: unknown): string {
  if (error instanceof ApiError && error.code === "invalid_provider" && error.reason !== undefined) {
    const key = `identity.reason.${error.reason}`;
    if (key in catalogues.en) {
      return t(key as MessageKey);
    }
  }
  return errorMessage(t, error);
}
