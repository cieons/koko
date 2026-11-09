import { useAppShellMobile } from "@astryxdesign/core/AppShell";
import { Avatar } from "@astryxdesign/core/Avatar";
import {
  DropdownMenu,
  DropdownMenuDivider,
  DropdownMenuGroup,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from "@astryxdesign/core/DropdownMenu";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { CircleUser, Monitor, Moon, Sun } from "lucide-react";

import { type Language, languages } from "../i18n/catalogues";
import type { Caller } from "./caller";
import { type Translate, useLanguageChoice, useT } from "./i18n";
import { type Mode, modes, useMode } from "./mode";

const modeIcons = { light: Sun, dark: Moon, system: Monitor } as const;

/** whoText says whom the web UI acts as, and from which device. */
function whoText(t: Translate, caller: Caller): string {
  const user = caller.user.name;
  return caller.deviceSession
    ? t("shell.header.device", { user, device: caller.deviceSession.device })
    : t("shell.header.local", { user });
}

/**
 * AccountMenu is the top bar's end: whom the web UI acts as, beside the
 * account menu's button on a desktop and at the head of its menu on a
 * phone, where the bar has no room for it; and the menu, which chooses the
 * language and the mode (Light, Dark or System), each kept by the browser.
 * Before user.current answers it chooses them still, for a notice to read.
 */
export function AccountMenu({ caller }: { caller: Caller | undefined }) {
  const t = useT();
  const { isMobile } = useAppShellMobile();
  const [language, setLanguage] = useLanguageChoice();
  const [mode, setMode] = useMode();
  const who = caller && whoText(t, caller);
  return (
    <HStack gap={3} vAlign="center">
      {who && !isMobile && (
        <Text type="body" color="secondary" maxLines={1}>
          {who}
        </Text>
      )}
      <DropdownMenu
        alignment="end"
        renderTrigger={(trigger) => (
          <IconButton
            {...trigger}
            variant="ghost"
            label={t("shell.account")}
            tooltip={t("shell.account")}
            icon={caller ? <Avatar name={caller.user.name} size="sm" tooltip={false} /> : <CircleUser />}
          />
        )}
      >
        {who && isMobile && (
          <>
            <DropdownMenuGroup title={who}>{null}</DropdownMenuGroup>
            <DropdownMenuDivider />
          </>
        )}
        <DropdownMenuGroup title={t("shell.language")}>
          <DropdownMenuRadioGroup
            label={t("shell.language")}
            value={language}
            onChange={(next) => setLanguage(next as Language)}
          >
            {languages.map((l) => (
              <DropdownMenuRadioItem key={l} value={l} label={<span lang={l}>{t(`shell.language.${l}`)}</span>} />
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
        <DropdownMenuDivider />
        <DropdownMenuGroup title={t("shell.mode")}>
          <DropdownMenuRadioGroup label={t("shell.mode")} value={mode} onChange={(next) => setMode(next as Mode)}>
            {modes.map((m) => (
              <DropdownMenuRadioItem key={m} value={m} label={t(`shell.mode.${m}`)} icon={modeIcons[m]} />
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenu>
    </HStack>
  );
}
