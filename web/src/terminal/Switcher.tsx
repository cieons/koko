import { Plus, Search } from "lucide-react";
import {
  Autocomplete,
  Dialog,
  Header,
  Input,
  ListBox,
  ListBoxItem,
  ListBoxSection,
  Modal,
  ModalOverlay,
  TextField,
  useFilter,
} from "react-aria-components";

import type { Host } from "../api/host";
import { useT } from "../shell/i18n";
import { AlertLine } from "../ui/states";
import { refusalSaid } from "./said";
import { chord } from "./keys";
import { shortPath } from "./RailSection";
import { stateWord, TabMark } from "./TabMark";
import { maxTabs, openTab, setSwitcher, showTab, tabStatus, useWorkspace } from "./workspace";

/** itemClass is a row of the switcher's list, marked while the virtual focus is on it. */
const itemClass =
  "flex min-h-12 cursor-default items-center gap-3 rounded-xl px-3 py-2 outline-none data-[focused]:bg-default";

/**
 * Switcher finds a tab or opens a Host (docs/web-design.md, "The
 * switcher"): one field over one list of two groups, the open tabs and the
 * Managed Hosts, narrowed by name, directory and Space as it is typed in.
 * The arrow keys move, Enter shows the tab or opens the Host in a new tab,
 * Escape closes. At 16 tabs the Hosts give way to the limit's line. A
 * Host opened closes it once the Workspace names its tab; an open refused
 * leaves it open, saying why.
 */
export function Switcher({ hosts }: { hosts: readonly Host[] | undefined }) {
  const t = useT();
  const { tabs, active, openRefused } = useWorkspace();
  const { contains } = useFilter({ sensitivity: "base" });
  const full = tabs.length >= maxTabs;
  const close = () => setSwitcher(false);
  const act = (key: string) => {
    const [kind, value = ""] = key.split(":");
    if (kind === "tab") {
      showTab(value, { focus: true, person: true });
      close();
    } else {
      void openTab(value).then((opened) => opened && close());
    }
  };
  const refused = openRefused && refusalSaid(t, openRefused.host, openRefused.code, openRefused.message);
  return (
    <ModalOverlay
      isOpen
      isDismissable
      onOpenChange={(open) => open || close()}
      className="fixed inset-0 z-50 flex items-start justify-center bg-[var(--backdrop)] px-3 pt-[12vh] max-md:pt-3"
    >
      <Modal className="w-full max-w-[36rem] outline-none">
        <Dialog
          aria-label={t("terminal.switcher.title")}
          className="flex max-h-[76vh] flex-col overflow-hidden rounded-[1.25rem] bg-overlay shadow-overlay outline-none"
        >
          <Autocomplete filter={contains}>
            <TextField aria-label={t("terminal.switcher.title")} autoFocus className="flex items-center gap-3 border-b border-line px-5">
              <Search aria-hidden className="size-5 shrink-0 text-muted" />
              <Input
                placeholder={t("terminal.switcher.hint")}
                autoComplete="off"
                spellCheck={false}
                className="h-14 min-w-0 flex-1 bg-transparent text-[1.0625rem] outline-none placeholder:text-muted max-md:text-base"
              />
            </TextField>
            <ListBox
              aria-label={t("terminal.switcher.title")}
              onAction={(key) => act(String(key))}
              renderEmptyState={() => <p className="px-3 py-2 text-muted">{t("terminal.switcher.none")}</p>}
              className="min-h-0 flex-1 overflow-y-auto p-2 outline-none"
            >
              {tabs.length > 0 && (
                <ListBoxSection>
                  <Header className="t-label px-3 pt-1 pb-1.5">{t("terminal.switcher.tabs")}</Header>
                  {tabs.map((tab) => (
                    <ListBoxItem
                      key={`tab:${tab.id}`}
                      id={`tab:${tab.id}`}
                      textValue={`${tab.label} ${tab.cwd ?? ""}`}
                      className={itemClass}
                    >
                      <span className="flex w-4 shrink-0 justify-center">
                        <TabMark status={tabStatus(tab)} word={false} />
                      </span>
                      <span className="t-name min-w-0 truncate">{tab.label}</span>
                      <span className="t-label min-w-0 flex-1 truncate font-mono text-xs">
                        {tabStatus(tab) === "connected"
                          ? tab.cwd === undefined
                            ? ""
                            : shortPath(tab.cwd)
                          : stateWord(t, tabStatus(tab))}
                      </span>
                      {tab.id === active && <span className="t-label shrink-0">{t("terminal.switcher.shown")}</span>}
                    </ListBoxItem>
                  ))}
                </ListBoxSection>
              )}
              {!full && (hosts?.length ?? 0) > 0 && (
                <ListBoxSection>
                  <Header className="t-label px-3 pt-3 pb-1.5">{t("terminal.switcher.hosts")}</Header>
                  {(hosts ?? []).map((host) => (
                    <ListBoxItem
                      key={`host:${host.name}`}
                      id={`host:${host.name}`}
                      textValue={`${host.name} ${host.space}`}
                      className={itemClass}
                    >
                      <span aria-hidden className="flex w-4 shrink-0 justify-center text-muted">
                        <Plus className="size-3.5" />
                      </span>
                      <span className="t-name min-w-0 truncate">{host.name}</span>
                      <span className="t-label min-w-0 flex-1 truncate">{host.space}</span>
                      <span className="t-label shrink-0">{t("terminal.switcher.open")}</span>
                    </ListBoxItem>
                  ))}
                </ListBoxSection>
              )}
            </ListBox>
          </Autocomplete>
          {refused && (
            <div className="border-t border-line px-5 py-3">
              <AlertLine>
                {refused.line}
                {refused.detail !== undefined && <span className="t-code mt-1 block text-xs break-words">{refused.detail}</span>}
              </AlertLine>
            </div>
          )}
          {full && <p className="t-label border-t border-line px-5 py-3">{t("terminal.tab.limit", { max: maxTabs })}</p>}
          <p className="t-label hidden border-t border-line px-5 py-3 md:block">
            {t("terminal.switcher.keys", { open: chord("K"), left: chord("←"), right: chord("→") })}
          </p>
        </Dialog>
      </Modal>
    </ModalOverlay>
  );
}
