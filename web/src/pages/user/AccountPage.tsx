import { Button } from "@heroui/react";
import { LogOut } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { type Language, languages, type MessageKey } from "../../i18n/catalogues";
import { browserName } from "../../shell/browserName";
import { type Caller, roleName, useCaller } from "../../shell/caller";
import { reloadReads } from "../../shell/data";
import { type Translate, useLanguageChoice, useT } from "../../shell/i18n";
import { type Mode, modes, useMode } from "../../shell/mode";
import { useQueryFlag, useSearchParams } from "../../shell/router";
import { useSignOut } from "../../shell/signOut";
import { Segments } from "../../ui/fields";
import { notify } from "../../ui/notify";
import { Page, Sheet } from "../../ui/Page";
import { Refusal } from "../../ui/states";
import { Devices } from "../device/Devices";
import { ChangePassword } from "../signin/ChangePassword";
import { LinkedIdentity } from "../signin/LinkedIdentity";
import { type Methods, useMethods } from "../signin/methods";
import { Passkeys } from "../signin/Passkeys";
import { refusedFrom } from "../signin/provider";
import { SignIns } from "../signin/SignIns";
import { passkeysAvailable } from "../signin/webauthn";
import { useWebURL } from "../signin/webURL";
import { PersonalSpace } from "./PersonPanel";

/**
 * AccountPage is Account (docs/web-design.md, "Account"): who the web UI
 * acts as, their role and how this browser got in, with which way to sign
 * in; their ways to sign in (SignInPanel); the two choices this browser
 * keeps, Language and Appearance, mirroring the rail's; the caller's own
 * devices, this one marked, the Revoked ones too with all=1; their Browser
 * Sign-ins, this browser's marked; and what their Personal Space holds. A
 * browser signed in signs out from the title. The Identity Provider's
 * callback comes back to it after linking: with ?linked=1, which a toast
 * confirms, or ?refused=<code>, shown in the Sign-in panel; either leaves
 * the URL once read, in place of its history entry.
 */
export function AccountPage() {
  const t = useT();
  const caller = useCaller();
  const [all, setAll] = useQueryFlag("all");
  const [changes, setChanges] = useState(0);
  const signOut = useSignOut();
  const { name } = caller.user;
  // Only a browser on the web listener asks /auth/methods; koko web has no /auth/ endpoints.
  const methods = useMethods(caller.signin !== undefined);
  const refused = useCallbackOutcome();
  return (
    <Page
      name={t("user.account.title")}
      title={
        <span className="flex flex-wrap items-baseline gap-x-3">
          {name}
          <span className="t-label tracking-normal">{t(roleName(caller.user))}</span>
        </span>
      }
      lead={viaText(t, caller)}
      actions={
        caller.signin && (
          <Button variant="secondary" isPending={signOut.pending} onPress={signOut.signOut}>
            <LogOut aria-hidden className="size-4" />
            {t("signin.signOut")}
          </Button>
        )
      }
    >
      {signOut.error !== undefined && (
        <div className="mb-5">
          <Refusal error={signOut.error} />
        </div>
      )}
      {/* Two columns once the page's own room allows, beside the rail as on a narrower window. Until
          then one column as wide as the page, not as its widest panel, which grows with the text's size. */}
      <div className="@container">
        <div className="grid grid-cols-1 items-start gap-5 @4xl:grid-cols-2">
          <div className="flex flex-col gap-5">
            <SignInPanel
              caller={caller}
              methods={methods}
              refused={refused}
              onChanged={() => setChanges((n) => n + 1)}
            />
            <ThisBrowser />
          </div>
          <div className="flex flex-col gap-5">
            <Devices user={name} owner={caller.owner} all={all} onAll={setAll} level={2} className="sheet p-6" />
            <SignIns user={name} level={2} changes={changes} provider={methods?.provider} className="sheet p-6" />
            <PersonalSpacePanel name={name} />
          </div>
        </div>
      </div>
    </Page>
  );
}

/**
 * useCallbackOutcome reads what the Identity Provider's callback came back
 * to Account with, once: ?linked=1, said in a toast, and ?refused=<code>,
 * which it answers for the Sign-in panel to show; then it takes both out of
 * the URL, in place of its history entry, so a reload says neither again.
 */
function useCallbackOutcome(): unknown {
  const t = useT();
  const [params, setParams] = useSearchParams();
  const [refused] = useState(() => refusedFrom(params.get("refused")));
  // The toast is said once, though a development build runs the effect twice.
  const told = useRef(false);
  useEffect(() => {
    if (!params.has("linked") && !params.has("refused")) {
      return;
    }
    if (params.get("linked") === "1" && !told.current) {
      told.current = true;
      notify(t("identity.linked.done"));
    }
    const rest = new URLSearchParams(params);
    rest.delete("linked");
    rest.delete("refused");
    setParams(rest, { replace: true });
    // Read as the URL changes, not for a new t or setParams.
  }, [params]);
  return refused;
}

/**
 * SignInPanel is the caller's ways to sign in: their passkeys, with Add a
 * passkey for a browser signed in with WebAuthn where /auth/methods says
 * passkeys work at the Web URL, else a sentence saying they need a domain
 * name; through koko web or for the Local caller a sentence saying where
 * one is added, and an identity linked; their Password, changed there, or
 * for a User without one set there, for a browser signed in; else one
 * sentence saying where; and their Linked Identity, with Link <name> for a
 * browser signed in where the Identity Provider is configured, and the
 * callback's refusal (refused). onChanged hears a Password changed, which
 * ends the other Sign-ins.
 */
function SignInPanel({
  caller,
  methods,
  refused,
  onChanged,
}: {
  caller: Caller;
  /** methods are what /auth/methods answered, undefined until then, and for a page that cannot ask it. */
  methods: Methods | undefined;
  refused: unknown;
  onChanged: () => void;
}) {
  const t = useT();
  const { user, signin } = caller;
  const webURL = useWebURL();
  const add =
    signin === undefined
      ? "elsewhere"
      : !passkeysAvailable() || methods === undefined
        ? "none"
        : methods.passkeys
          ? "here"
          : "unusable";
  const first = user.password_set_at === undefined;
  return (
    <Sheet title={t("signin.methods")}>
      <div className="flex flex-col gap-6">
        <Passkeys
          user={user.name}
          webHost={webURL.host}
          level={3}
          acts
          otherWays={!first || user.identity_linked_at !== undefined}
          add={add}
        />
        <section className="flex flex-col gap-3">
          <h3 className="t-heading">{t("signin.password")}</h3>
          {signin ? (
            <ChangePassword
              user={user.name}
              first={first}
              onChanged={() => {
                onChanged();
                // A first Password changes the User the page acts as: Account then offers to change it.
                if (first) {
                  reloadReads();
                }
              }}
            />
          ) : (
            <p className="text-muted">
              {t(caller.deviceSession ? "signin.change.viaDevice" : "signin.change.viaLocal")}
            </p>
          )}
        </section>
        <LinkedIdentity
          user={user}
          provider={methods?.provider}
          level={3}
          acts
          link={signin !== undefined}
          refused={refused}
          onUnlinked={reloadReads}
        />
      </div>
    </Sheet>
  );
}

/** viaKeys are the line saying how this browser signed in, by its Sign-in's method; one this web UI does not know names none. */
const viaKeys: Readonly<Record<string, MessageKey>> = {
  password: "user.account.viaPassword",
  passkey: "user.account.viaPasskey",
  identity: "user.account.viaIdentity",
};

/**
 * viaText says how this browser got in: through koko web on a device, a
 * Sign-in and its way, or the koko server's own machine. A koko server
 * before passkeys names no way, and had only Passwords.
 */
function viaText(t: Translate, caller: Caller): string {
  if (caller.deviceSession) {
    return t("user.account.viaDevice", { device: caller.deviceSession.device });
  }
  if (caller.signin) {
    return t(viaKeys[caller.signin.method ?? "password"] ?? "user.account.viaSignin", {
      client: browserName(t, caller.signin),
    });
  }
  return t("user.account.viaLocal");
}

/** ThisBrowser holds the two choices this browser keeps, as segments that mirror the rail's menus. */
function ThisBrowser() {
  const t = useT();
  const [language, setLanguage] = useLanguageChoice();
  const [mode, setMode] = useMode();
  return (
    <Sheet title={t("user.account.browser")} className="flex flex-col gap-5">
      <Segments
        label={t("shell.language")}
        value={language}
        onChange={(l) => setLanguage(l as Language)}
        options={languages.map((l) => ({ id: l, label: t(`shell.language.${l}`) }))}
      />
      <Segments
        label={t("shell.mode")}
        value={mode}
        onChange={(m) => setMode(m as Mode)}
        options={modes.map((m) => ({ id: m, label: t(`shell.mode.${m}`) }))}
      />
    </Sheet>
  );
}

/** PersonalSpacePanel is the caller's Personal Space: what it holds, with a link to its Hosts. */
function PersonalSpacePanel({ name }: { name: string }) {
  const t = useT();
  return (
    <Sheet title={t("space.personal")}>
      <PersonalSpace name={name} />
    </Sheet>
  );
}
