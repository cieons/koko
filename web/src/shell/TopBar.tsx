import { Button, Dropdown, Label, Popover } from "@heroui/react";
import { ChevronDown, Languages, Monitor, Moon, Sun, UserRound } from "lucide-react";
import { type ReactNode, useState } from "react";
import { Link } from "react-aria-components";

import { type Language, languages } from "../i18n/catalogues";
import type { Caller } from "./caller";
import { type Translate, useLanguageChoice, useT } from "./i18n";
import { type Mode, modes, useMode } from "./mode";
import { accountPage } from "./router";

// The top bar's end: the two choices the browser keeps, Language and
// Appearance, each an icon button that opens a menu of radio items, and the
// account button, which opens the account panel (docs/web-design.md, "The
// frame").

const modeIcons = { light: Sun, dark: Moon, system: Monitor } as const;

/** BarChoice is one of the browser's two choices: an icon button named for its value, opening its options. */
function BarChoice<T extends string>(props: {
  label: string;
  current: string;
  icon: ReactNode;
  value: T;
  onChange: (value: T) => void;
  options: readonly { id: T; label: string; lang?: string }[];
}) {
  return (
    <Dropdown>
      <Button variant="ghost" isIconOnly className="rounded-full text-ink-2" aria-label={props.current}>
        {props.icon}
      </Button>
      <Dropdown.Popover placement="bottom end" className="min-w-44">
        <Dropdown.Menu
          aria-label={props.label}
          selectionMode="single"
          disallowEmptySelection
          selectedKeys={[props.value]}
          onSelectionChange={(keys) => {
            const chosen = props.options.find((o) => keys !== "all" && keys.has(o.id));
            if (chosen) {
              props.onChange(chosen.id);
            }
          }}
        >
          {props.options.map((o) => (
            <Dropdown.Item key={o.id} id={o.id} textValue={o.label}>
              <Label lang={o.lang}>{o.label}</Label>
              <Dropdown.ItemIndicator />
            </Dropdown.Item>
          ))}
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}

/** LanguageChoice chooses the web UI's language, which the browser keeps. */
export function LanguageChoice() {
  const t = useT();
  const [language, setLanguage] = useLanguageChoice();
  return (
    <BarChoice<Language>
      label={t("shell.language")}
      current={t("shell.language.current", { language: t(`shell.language.${language}`) })}
      icon={<Languages aria-hidden className="size-[1.125rem]" />}
      value={language}
      onChange={setLanguage}
      options={languages.map((l) => ({ id: l, label: t(`shell.language.${l}`), lang: l }))}
    />
  );
}

/** ModeChoice chooses Light, Dark or System, which the browser keeps; its icon shows the choice. */
export function ModeChoice() {
  const t = useT();
  const [mode, setMode] = useMode();
  const Icon = modeIcons[mode];
  return (
    <BarChoice<Mode>
      label={t("shell.mode")}
      current={t("shell.mode.current", { mode: t(`shell.mode.${mode}`) })}
      icon={<Icon aria-hidden className="size-[1.125rem]" />}
      value={mode}
      onChange={setMode}
      options={modes.map((m) => ({ id: m, label: t(`shell.mode.${m}`) }))}
    />
  );
}

/**
 * Initial is a name's first letter in a circle, beside the name. The letter
 * is drawn from data-initial, not written as text, so a button's visible
 * text stays the name its accessible name holds.
 */
export function Initial({ name, className = "size-7 text-xs" }: { name: string; className?: string }) {
  return (
    <span
      aria-hidden
      data-initial={name.slice(0, 1).toUpperCase()}
      className={`inline-flex shrink-0 items-center justify-center rounded-full bg-default font-semibold shadow-[inset_0_0_0_1px_var(--border)] before:content-[attr(data-initial)] ${className}`}
    />
  );
}

/** viaText says how this browser reaches koko: a Device Session's device, a Sign-in's browser, or the koko server's machine. */
function viaText(t: Translate, caller: Caller): string {
  if (caller.deviceSession) {
    return t("shell.via.device", { device: caller.deviceSession.device });
  }
  if (caller.signin) {
    return t("shell.via.signin", { client: caller.signin.client });
  }
  return t("shell.via.local");
}

/**
 * AccountPanel is the account button, the User's initial and name (the name
 * hidden on a phone) with a chevron, and the panel it opens: the initial,
 * the name and role, how this browser reaches koko, and a link to Account,
 * which closes the panel as it is followed. Sign out joins it with its page
 * (#116).
 */
export function AccountPanel({ caller }: { caller: Caller }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const { name } = caller.user;
  const label = t("shell.account", { name });
  return (
    <Popover isOpen={open} onOpenChange={setOpen}>
      <Button variant="ghost" className="h-10 gap-2 rounded-full pr-2.5 pl-1.5" aria-label={label}>
        <Initial name={name} />
        <span className="hidden font-medium sm:inline">{name}</span>
        <ChevronDown aria-hidden className="size-4 text-muted" />
      </Button>
      <Popover.Content placement="bottom end" className="w-[19rem] rounded-2xl p-0">
        <Popover.Dialog className="p-0" aria-label={label}>
          <div className="flex items-center gap-3 px-4 pt-4 pb-4">
            <Initial name={name} className="size-10 text-base" />
            <div className="min-w-0">
              <p className="flex items-baseline gap-2">
                <span className="t-name truncate">{name}</span>
                <span className="t-label">{t(caller.owner ? "shell.role.owner" : "shell.role.member")}</span>
              </p>
              <p className="t-label truncate">{viaText(t, caller)}</p>
            </div>
          </div>
          <div className="border-t border-line p-1.5">
            <Link
              href={accountPage}
              onPress={() => setOpen(false)}
              className="flex h-10 items-center gap-3 rounded-xl px-3 text-sm font-medium text-foreground no-underline outline-none hover:bg-default data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus"
            >
              <UserRound aria-hidden className="size-4 text-muted" />
              {t("user.account.title")}
            </Link>
          </div>
        </Popover.Dialog>
      </Popover.Content>
    </Popover>
  );
}
