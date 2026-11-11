import { Button, Link } from "@heroui/react";
import { CircleArrowUp } from "lucide-react";
import { createContext, useContext, useId, useState } from "react";

import { type Server, ServerCheckUpdate, type ServerCheckUpdateResponse, ServerShow } from "../api/server";
import { call } from "../client";
import { CommandLine } from "../ui/copy";
import { Refusal } from "../ui/states";
import { useRead } from "./data";
import { formatAgo } from "./format";
import { type Translate, useLanguage, useT } from "./i18n";
import { isNewer } from "./version";

// What the shell tells the Owner, and only the Owner, of koko's releases
// (spec #136, "The web UI"; ADR 0048): the notice above every page while a
// release newer than the koko server's version is known, and the koko
// server's line in the account panel, each with Check now.

/** laterKey is where the browser keeps the release Later hid the notice for. */
const laterKey = "koko.release.later";

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
 * useReleaseState reads server.show for the Owner, as a read reloads on each
 * visit and on focus, and asks nothing for a Member, who has no release to
 * act on.
 */
export function useReleaseState(owner: boolean): Release {
  const show = useRead(ServerShow, {}, { enabled: owner });
  const [later, setLater] = useState(storedLater);
  const hide = (version: string) => {
    setLater(version);
    try {
      localStorage.setItem(laterKey, version);
    } catch {
      // the notice stays hidden in this page alone
    }
  };
  return { server: show.data?.server, reload: show.reload, later, hide };
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
