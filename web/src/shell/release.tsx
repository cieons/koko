import { Button, Link, Spinner } from "@heroui/react";
import { CircleArrowUp, CircleCheck, CircleX } from "lucide-react";
import { createContext, useContext, useEffect, useId, useRef, useState } from "react";

import {
  type LastUpgrade,
  type Server,
  ServerCheckUpdate,
  type ServerCheckUpdateResponse,
  ServerShow,
  ServerUpgrade,
} from "../api/server";
import { call } from "../client";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { CommandLine } from "../ui/copy";
import { Refusal } from "../ui/states";
import { useRead, useWrite } from "./data";
import { formatAgo } from "./format";
import { type Translate, useLanguage, useT } from "./i18n";
import { reloadPage } from "./reloadPage";
import { isNewer } from "./version";

// What the shell tells the Owner, and only the Owner, of koko's releases
// (spec #136, "The web UI"; ADR 0048): the notice above every page while a
// release newer than the koko server's version is known, and the koko
// server's line in the account panel, each with Check now; and, for a koko
// server installed with systemd, its upgrade, from the confirmation to the
// outcome.

/** laterKey is where the browser keeps the release Later hid the notice for. */
const laterKey = "koko.release.later";

/** upgradePoll is how often the page asks server.show while the koko server upgrades, in milliseconds. */
export const upgradePoll = 2000;

/**
 * upgradeWait is how long the page follows an upgrade before it says where
 * to look instead: 15 minutes, which covers the helper's 10-minute bound on
 * the download, as koko server upgrade waits.
 */
export const upgradeWait = 15 * 60 * 1000;

/** reloadAfter is how long "The koko server runs vX.Y.Z" shows before the page reloads. */
export const reloadAfter = 2000;

/**
 * Upgrade is where the upgrade asked for in this page stands: none; running,
 * from the outcome before it (before) until the helper records another for
 * the release asked for; succeeded, failed with the outcome's message and
 * the version that runs, or no outcome within upgradeWait.
 */
export type Upgrade =
  | { state: "none" }
  | { state: "running"; to: string; before: LastUpgrade | null }
  | { state: "succeeded"; to: string }
  | { state: "failed"; to: string; version: string; message: string | null }
  | { state: "timedOut"; to: string };

/** Release is what the shell knows of the koko server's version and koko's releases. */
export interface Release {
  /** server is the koko server as server.show last answered; undefined for a Member and until it answers. */
  server?: Server;
  /** reload asks server.show again, as after a check. */
  reload: () => void;
  /** later is the release whose notice Later hid in this browser. */
  later?: string;
  /** hide hides the notice for the release version, in this browser. */
  hide: (version: string) => void;
  /** upgrade is where the upgrade asked for in this page stands. */
  upgrade: Upgrade;
  /** follow follows the upgrade to the release to, asked for when the last outcome was before. */
  follow: (to: string, before: LastUpgrade | null) => void;
  /** dismiss puts an ended upgrade's outcome away, and the notice back while a newer release is known. */
  dismiss: () => void;
}

const ReleaseContext = createContext<Release | undefined>(undefined);

/** ReleaseProvider gives the shell's parts what useRelease reads. */
export const ReleaseProvider = ReleaseContext;

/** useRelease is what the shell knows of koko's releases, within the frame. */
function useRelease(): Release {
  const release = useContext(ReleaseContext);
  if (!release) {
    throw new Error("a release notice is outside the frame's ReleaseProvider");
  }
  return release;
}

/** storedLater is the release Later hid, as the browser keeps it. */
function storedLater(): string | undefined {
  try {
    return localStorage.getItem(laterKey) ?? undefined;
  } catch {
    return undefined; // storage the browser refuses keeps nothing
  }
}

/**
 * useReleaseState reads server.show for the Owner and an Administrator
 * (shown), as a read reloads on each visit and on focus, and asks nothing
 * for a Member, who has no release to act on. It follows an upgrade asked for in this page.
 */
export function useReleaseState(shown: boolean): Release {
  const show = useRead(ServerShow, {}, { enabled: shown });
  const [later, setLater] = useState(storedLater);
  const [upgrade, setUpgrade] = useState<Upgrade>({ state: "none" });
  useFollowUpgrade(upgrade, setUpgrade);
  const hide = (version: string) => {
    setLater(version);
    try {
      localStorage.setItem(laterKey, version);
    } catch {
      // the notice stays hidden in this page alone
    }
  };
  return {
    server: show.data?.server,
    reload: show.reload,
    later,
    hide,
    upgrade,
    follow: (to, before) => setUpgrade({ state: "running", to, before }),
    dismiss: () => {
      setUpgrade({ state: "none" });
      show.reload();
    },
  };
}

/** sameOutcome tells whether a and b are the same outcome of an upgrade, as koko server upgrade tells it. */
function sameOutcome(a: LastUpgrade | null, b: LastUpgrade | null): boolean {
  if (a === null || b === null) {
    return a === b;
  }
  return (
    a.state === b.state &&
    a.from === b.from &&
    a.to === b.to &&
    a.started_at === b.started_at &&
    a.finished_at === b.finished_at
  );
}

/**
 * useFollowUpgrade follows a running upgrade: it asks server.show every 2
 * seconds until last_upgrade holds an outcome other than the one before,
 * for the release asked for, that no longer runs. The calls that fail while
 * the koko server restarts, a 502 from the reverse proxy or a koko web that
 * reconnects, are expected and asked again. After 15 minutes with no
 * outcome it stops. Once the upgrade succeeded, the page reloads, since the
 * web UI's own files changed with kokod.
 */
function useFollowUpgrade(upgrade: Upgrade, setUpgrade: (upgrade: Upgrade) => void): void {
  useEffect(() => {
    if (upgrade.state !== "running") {
      return;
    }
    const { to, before } = upgrade;
    let ended = false;
    let poll: ReturnType<typeof setTimeout> | undefined;
    const end = (next: Upgrade) => {
      ended = true;
      clearTimeout(poll);
      clearTimeout(deadline);
      setUpgrade(next);
    };
    const deadline = setTimeout(() => end({ state: "timedOut", to }), upgradeWait);
    const ask = () => {
      call(ServerShow, {})
        .then(
          ({ server }) => {
            const o = server.last_upgrade;
            if (!ended && o !== null && o.to === to && o.state !== "running" && !sameOutcome(o, before)) {
              end(
                o.state === "succeeded"
                  ? { state: "succeeded", to }
                  : { state: "failed", to, version: server.version, message: o.message },
              );
            }
          },
          () => undefined, // the koko server restarts
        )
        .finally(() => {
          if (!ended) {
            poll = setTimeout(ask, upgradePoll);
          }
        });
    };
    poll = setTimeout(ask, upgradePoll);
    return () => {
      ended = true;
      clearTimeout(poll);
      clearTimeout(deadline);
    };
  }, [upgrade, setUpgrade]);

  useEffect(() => {
    if (upgrade.state !== "succeeded") {
      return;
    }
    const timer = setTimeout(reloadPage, reloadAfter);
    return () => clearTimeout(timer);
  }, [upgrade]);
}

/** newerRelease is the latest release the koko server's check found, when it is newer than the koko server. */
function newerRelease(server: Server | undefined): string | undefined {
  const latest = server?.update_check.latest;
  return server && latest && isNewer(latest, server.version) ? latest : undefined;
}

/** releaseNotes is the address of a release's notes on GitHub. */
function releaseNotes(version: string): string {
  return `https://github.com/cieons/koko/releases/tag/${version}`;
}

/** CheckNow is one place's Check now: its call, and what it found. */
interface CheckNow {
  check: () => void;
  pending: boolean;
  error?: unknown;
  found?: ServerCheckUpdateResponse;
}

/**
 * useCheckNow is a Check now button's state: it calls server.check_update,
 * which checks at once and changes nothing, and then asks server.show
 * again, so a newer release found brings the notice.
 */
function useCheckNow(release: Release): CheckNow {
  const [state, setState] = useState<{ pending: boolean; error?: unknown; found?: ServerCheckUpdateResponse }>({
    pending: false,
  });
  const check = () => {
    setState({ pending: true });
    call(ServerCheckUpdate, {}).then(
      (found) => {
        setState({ pending: false, found });
        release.reload();
      },
      (error: unknown) => setState({ pending: false, error }),
    );
  };
  return { check, ...state };
}

/**
 * ReleaseNotice is the notice above every page, for the Owner, while a
 * release newer than the koko server's version is known and Later has not
 * hidden it: which release, which version runs, a link to the release's
 * notes, Check now, Later, and how this koko server upgrades.
 */
export function ReleaseNotice() {
  const release = useRelease();
  if (release.upgrade.state !== "none") {
    return <UpgradeProgress upgrade={release.upgrade} dismiss={release.dismiss} />;
  }
  const latest = newerRelease(release.server);
  if (!release.server || latest === undefined || release.later === latest) {
    return null;
  }
  return <Notice release={release} server={release.server} latest={latest} />;
}

function Notice({ release, server, latest }: { release: Release; server: Server; latest: string }) {
  const t = useT();
  const titleId = useId();
  const checkNow = useCheckNow(release);
  const [howTo, setHowTo] = useState(false);
  const [confirming, setConfirming] = useState(false);
  return (
    <section aria-labelledby={titleId} className="sheet mt-6 flex flex-col gap-4 p-5 md:mt-9 md:p-6">
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-4">
        <div className="flex min-w-0 items-start gap-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent-soft-foreground">
            <CircleArrowUp aria-hidden className="size-5" />
          </span>
          <div className="min-w-0">
            <p id={titleId} className="t-name">
              {t("server.release.available", { latest })}
            </p>
            <p className="text-muted">
              {t("server.release.runs", { version: server.version })}{" "}
              <Link href={releaseNotes(latest)} target="_blank" rel="noopener noreferrer">
                {t("server.release.notes")}
                <Link.Icon />
              </Link>
            </p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="ghost" isPending={checkNow.pending} onPress={checkNow.check}>
            {t("server.check.now")}
          </Button>
          <Button variant="ghost" onPress={() => release.hide(latest)}>
            {t("server.release.later")}
          </Button>
          {server.upgrade_way === "systemd" && (
            <Button variant="secondary" onPress={() => setConfirming(true)}>
              {t("server.release.upgrade")}
            </Button>
          )}
          {server.upgrade_way === "image" && (
            <Button variant="secondary" aria-expanded={howTo} onPress={() => setHowTo((open) => !open)}>
              {t("server.release.howTo")}
            </Button>
          )}
        </div>
      </div>
      <p role="status" className="sr-only">
        {checkNow.pending ? t("server.check.checking") : ""}
      </p>
      <Refusal error={checkNow.error} />
      {server.upgrade_way === "image" && howTo && <ComposeLines latest={latest} />}
      {server.upgrade_way === "none" && <p className="text-muted">{t("server.release.none", { latest })}</p>}
      {confirming && <UpgradeConfirm release={release} to={latest} close={() => setConfirming(false)} />}
    </section>
  );
}

/**
 * UpgradeConfirm asks before the koko server upgrades to the release to,
 * saying that it restarts and every open SSH Session through it ends. Upgrade
 * and restart reads server.show's last outcome, then calls server.upgrade
 * for that release, and the page follows the upgrade; a refusal shows in
 * the dialog.
 */
function UpgradeConfirm({ release, to, close }: { release: Release; to: string; close: () => void }) {
  const t = useT();
  const ask = useWrite(ServerUpgrade);
  const [reading, setReading] = useState<{ pending: boolean; error?: unknown }>({ pending: false });
  const confirm = async () => {
    setReading({ pending: true });
    let before: LastUpgrade | null;
    try {
      before = (await call(ServerShow, {})).server.last_upgrade;
    } catch (error) {
      setReading({ pending: false, error });
      return;
    }
    setReading({ pending: false });
    const asked = await ask.submit({ version: to });
    if (asked) {
      release.follow(asked.to, before);
    }
  };
  return (
    <ConfirmDialog
      title={t("server.upgrade.title", { version: to })}
      consequence={t("server.upgrade.consequence", { version: to })}
      confirm={t("server.upgrade.confirm")}
      pending={reading.pending || ask.pending}
      error={reading.error ?? ask.error}
      onConfirm={() => void confirm()}
      onCancel={close}
    />
  );
}

/**
 * UpgradeProgress stands in the notice's place while the upgrade asked for
 * runs and once it has ended: that it runs; that the koko server runs the
 * new release, before the page reloads; why it failed, with the version
 * that runs; or, after 15 minutes with no outcome, where to look. It takes
 * the focus as it comes, since the confirmation it follows has gone.
 */
function UpgradeProgress({ upgrade, dismiss }: { upgrade: Exclude<Upgrade, { state: "none" }>; dismiss: () => void }) {
  const t = useT();
  const titleId = useId();
  const panel = useRef<HTMLElement>(null);
  useEffect(() => {
    panel.current?.focus();
  }, []);
  const ended = upgrade.state === "failed" || upgrade.state === "timedOut";
  return (
    <section
      ref={panel}
      tabIndex={-1}
      aria-labelledby={titleId}
      className="sheet mt-6 flex flex-col gap-4 p-5 outline-none md:mt-9 md:p-6"
    >
      <div role={ended ? "alert" : "status"} className="flex items-start gap-3">
        <span
          className={`flex size-9 shrink-0 items-center justify-center rounded-full ${
            ended ? "bg-danger-soft text-danger-soft-foreground" : "bg-accent-soft text-accent-soft-foreground"
          }`}
        >
          {upgrade.state === "running" ? (
            <Spinner aria-hidden size="sm" color="current" />
          ) : upgrade.state === "succeeded" ? (
            <CircleCheck aria-hidden className="size-5" />
          ) : (
            <CircleX aria-hidden className="size-5" />
          )}
        </span>
        <div className="flex min-w-0 flex-col gap-1">
          <p id={titleId} className="t-name">
            {upgrade.state === "running"
              ? t("server.upgrade.running", { version: upgrade.to })
              : upgrade.state === "succeeded"
                ? t("server.upgrade.succeeded", { version: upgrade.to })
                : upgrade.state === "failed"
                  ? t("server.upgrade.failed", { to: upgrade.to, version: upgrade.version })
                  : t("server.upgrade.timedOut", { to: upgrade.to })}
          </p>
          {upgrade.state === "failed" && upgrade.message !== null && (
            <p className="t-code text-muted">{upgrade.message}</p>
          )}
        </div>
      </div>
      {upgrade.state === "timedOut" && (
        <CommandLine label={t("server.upgrade.logLabel")} command="journalctl -u koko -u koko-upgrade" />
      )}
      {ended && (
        <div>
          <Button variant="secondary" onPress={dismiss}>
            {t("shell.close")}
          </Button>
        </div>
      )}
    </section>
  );
}

/**
 * ComposeLines are how a koko server in the container image upgrades: the
 * release in .env, a step that a .env setting KOKO_VERSION=latest skips,
 * then Compose's pull and up. koko cannot see the tag it was started from,
 * so both lines show, and the pull serves either case.
 */
function ComposeLines({ latest }: { latest: string }) {
  const t = useT();
  return (
    <div className="flex flex-col gap-3">
      <p>{t("server.release.image.setVersion")}</p>
      <CommandLine
        label={t("server.release.image.setVersionLabel")}
        command={`KOKO_VERSION=${latest.replace(/^v/, "")}`}
        prompt={false}
      />
      <p>{t("server.release.image.pull")}</p>
      <CommandLine label={t("server.release.image.pullLabel")} command="docker compose pull && docker compose up -d" />
    </div>
  );
}

/** foundText says what a check found: a release newer than the koko server, or the newest release, which it runs. */
function foundText(t: Translate, { update_check: { latest }, version }: ServerCheckUpdateResponse): string {
  return latest !== null && isNewer(latest, version)
    ? t("server.release.available", { latest })
    : t("server.check.newest", { version: latest ?? version });
}

/**
 * ServerLine is the account panel's line for the Owner: the koko server's
 * version, when it last checked for a release, or why that check failed,
 * and Check now, whose outcome shows in place.
 */
export function ServerLine() {
  const t = useT();
  const language = useLanguage();
  const release = useRelease();
  const checkNow = useCheckNow(release);
  const { server } = release;
  if (!server) {
    return null;
  }
  const { checked_at: checkedAt, error } = server.update_check;
  const found = checkNow.found;
  return (
    <div className="flex flex-col gap-3 border-t border-line px-4 py-3">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="flex items-baseline gap-2">
            <span className="text-sm font-medium">{t("server.line.label")}</span>
            <span className="t-label">{server.version}</span>
          </p>
          <p className="t-label">
            {error !== null
              ? t("server.line.failed", { error })
              : checkedAt !== null
                ? t("server.line.checkedAgo", { ago: formatAgo(checkedAt, language) })
                : t("server.line.notChecked")}
          </p>
        </div>
        <Button size="sm" variant="secondary" isPending={checkNow.pending} onPress={checkNow.check}>
          {t("server.check.now")}
        </Button>
      </div>
      <p role="status" className={found || checkNow.pending ? "text-sm" : "sr-only"}>
        {checkNow.pending ? t("server.check.checking") : found ? foundText(t, found) : ""}
      </p>
      <Refusal error={checkNow.error} />
    </div>
  );
}
