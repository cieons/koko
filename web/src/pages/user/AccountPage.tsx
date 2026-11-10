import { type Language, languages } from "../../i18n/catalogues";
import { type Caller, useCaller } from "../../shell/caller";
import { type Translate, useLanguageChoice, useT } from "../../shell/i18n";
import { type Mode, modes, useMode } from "../../shell/mode";
import { useQueryFlag } from "../../shell/router";
import { Segments } from "../../ui/fields";
import { Page, Sheet } from "../../ui/Page";
import { Devices } from "../device/Devices";
import { PersonalSpace } from "./PersonPanel";

/**
 * AccountPage is Account (docs/web-design.md, "Account"): who the web UI
 * acts as, their role and how this browser got in; the two choices this
 * browser keeps, Language and Appearance, mirroring the top bar; the
 * caller's own devices, this one marked, the Revoked ones too with all=1;
 * and what their Personal Space holds. The Password and Sign-ins come with
 * #116.
 */
export function AccountPage() {
  const t = useT();
  const caller = useCaller();
  const [all, setAll] = useQueryFlag("all");
  const { name } = caller.user;
  return (
    <Page
      name={t("user.account.title")}
      title={
        <span className="flex flex-wrap items-baseline gap-x-3">
          {name}
          <span className="t-label tracking-normal">{t(caller.owner ? "shell.role.owner" : "shell.role.member")}</span>
        </span>
      }
      lead={viaText(t, caller)}
    >
      <div className="grid items-start gap-5 lg:grid-cols-2">
        <ThisBrowser />
        <div className="flex flex-col gap-5">
          <Devices
            user={name}
            owner={caller.owner}
            all={all}
            onAll={setAll}
            level={2}
            className="sheet p-6"
          />
          <PersonalSpacePanel name={name} />
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
    return t("user.account.viaSignin", { client: caller.signin.client });
  }
  return t("user.account.viaLocal");
}

/** ThisBrowser holds the two choices this browser keeps, as segments that mirror the top bar's menus. */
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
      <p className="t-label">{t("user.account.browserNote")}</p>
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
