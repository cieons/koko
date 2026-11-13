import { Button } from "@heroui/react";
import { LogOut } from "lucide-react";
import { useState } from "react";

import { type Language, languages } from "../../i18n/catalogues";
import { browserName } from "../../shell/browserName";
import { type Caller, roleName, useCaller } from "../../shell/caller";
import { type Translate, useLanguageChoice, useT } from "../../shell/i18n";
import { type Mode, modes, useMode } from "../../shell/mode";
import { useQueryFlag } from "../../shell/router";
import { useSignOut } from "../../shell/signOut";
import { Segments } from "../../ui/fields";
import { Page, Sheet } from "../../ui/Page";
import { Refusal } from "../../ui/states";
import { Devices } from "../device/Devices";
import { ChangePassword } from "../signin/ChangePassword";
import { SignIns } from "../signin/SignIns";
import { PersonalSpace } from "./PersonPanel";

/**
 * AccountPage is Account (docs/web-design.md, "Account"): who the web UI
 * acts as, their role and how this browser got in; the two choices this
 * browser keeps, Language and Appearance, mirroring the rail's; the
 * caller's own devices, this one marked, the Revoked ones too with all=1;
 * their Browser Sign-ins, this browser's marked; and what their Personal
 * Space holds. A browser signed in with a Password changes it in the
 * Password panel and signs out from the title; through koko web or for the
 * Local caller the panel says where a Password is changed instead.
 */
export function AccountPage() {
  const t = useT();
  const caller = useCaller();
  const [all, setAll] = useQueryFlag("all");
  const [changes, setChanges] = useState(0);
  const signOut = useSignOut();
  const { name } = caller.user;
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
            <Sheet title={t("signin.password")}>
              {caller.signin ? (
                <ChangePassword user={name} onChanged={() => setChanges((n) => n + 1)} />
              ) : (
                <p className="text-muted">
                  {t(caller.deviceSession ? "signin.change.viaDevice" : "signin.change.viaLocal")}
                </p>
              )}
            </Sheet>
            <ThisBrowser />
          </div>
          <div className="flex flex-col gap-5">
            <Devices user={name} owner={caller.owner} all={all} onAll={setAll} level={2} className="sheet p-6" />
            <SignIns user={name} level={2} changes={changes} className="sheet p-6" />
            <PersonalSpacePanel name={name} />
          </div>
        </div>
      </div>
    </Page>
  );
}

/** viaText says how this browser got in: through koko web on a device, a Sign-in, or the koko server's own machine. */
function viaText(t: Translate, caller: Caller): string {
  if (caller.deviceSession) {
    return t("user.account.viaDevice", { device: caller.deviceSession.device });
  }
  if (caller.signin) {
    return t("user.account.viaSignin", { client: browserName(t, caller.signin) });
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
