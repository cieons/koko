import { buttonVariants } from "@heroui/react";
import { CircleAlert, TriangleAlert } from "lucide-react";
import { type MouseEvent, type RefObject, useEffect, useRef } from "react";
import { Link } from "react-aria-components";
import { flushSync } from "react-dom";

import { UserCurrent } from "../api/user";
import { CodeSignInRequired } from "../api/web";
import { ApiError, observeCalls } from "../client";
import type { MessageKey } from "../i18n/catalogues";
import { Page as PageHead } from "../ui/Page";
import { usePhone, useSideways } from "../ui/phone";
import { useTerminalKeys } from "../terminal/keys";
import { CloseQuestion, WorkspaceNotices } from "../terminal/RailSection";
import { WorkspaceLayer } from "../terminal/routes";
import { resetWorkspace, startWorkspace, terminalPath, useWorkspace } from "../terminal/workspace";
import { Loading, LoadFailed } from "../ui/states";
import { CallerContext, callerOf } from "./caller";
import { useRead } from "./data";
import { useT } from "./i18n";
import { type Notice, useNotice } from "./notice";
import { Providers } from "./providers";
import { PhoneBar, PhoneTop, Rail, useRail } from "./Rail";
import { ReleaseNotice, ReleaseProvider, useReleaseState } from "./release";
import { matchRoute, navigate, type Route, signInPage, signInPath, useLocation } from "./router";

/**
 * Shell is the frame every page sits in, inside the Providers
 * (docs/web-design.md, "The frame"): a skip link; on a desktop the rail down
 * the start edge with koko's wordmark, the places the caller may see and, at
 * its foot, the account, Language and Appearance; on a phone a top row with
 * the wordmark and those three, and the places in a bar at the foot, which
 * on the terminal give way to the terminal's own phone layout, as the rail
 * does on a phone held sideways. In the
 * room beside the rail, the page's column: the shell's notice at its head,
 * then in the main region, for the Owner, the notice of a newer koko
 * release, then the page the URL names. A list page (`Route.fill`) has the
 * whole room; any other keeps a column 76rem wide at most, from the rail.
 * It learns who the web UI acts as from user.current, reloaded as every
 * read is, and renders a page only once it knows, so a Member never sees an
 * Owner-only page or place.
 */
export function Shell({ routes }: { routes: readonly Route[] }) {
  return (
    <Providers>
      <Root routes={routes} />
    </Providers>
  );
}

/**
 * Root renders a page outside the frame, as Sign in, on its own, and every
 * other page in the Frame.
 */
function Root({ routes }: { routes: readonly Route[] }) {
  const { pathname } = useLocation();
  const match = matchRoute(routes, pathname);
  if (match?.route.outside) {
    const { page: Outside } = match.route;
    return <Outside params={match.params} />;
  }
  return <Frame routes={routes} />;
}

/**
 * useSignInWhenRequired takes the browser to Sign in, replacing the history
 * entry, when any call is answered signin_required, a read or a write, as
 * on the web listener once a Sign-in has ended; Sign in then comes back to
 * this page. A second such answer, which may come before the frame is gone,
 * finds the browser on Sign in already and leaves it there.
 */
function useSignInWhenRequired(): void {
  useEffect(
    () =>
      observeCalls(({ error }) => {
        const { pathname, search } = window.location;
        if (error instanceof ApiError && error.code === CodeSignInRequired && pathname !== signInPage) {
          navigate(signInPath(pathname + search), { replace: true });
        }
      }),
    [],
  );
}

function Frame({ routes: all }: { routes: readonly Route[] }) {
  const t = useT();
  const routes = all.filter((r) => !r.outside);
  useSignInWhenRequired();
  const phone = usePhone();
  const sideways = useSideways();
  const rail = useRail();
  const userCurrent = useRead(UserCurrent, {});
  const caller = userCurrent.data && callerOf(userCurrent.data);
  const release = useReleaseState(caller?.administers === true);
  // While the koko server restarts for an upgrade, calls through koko web
  // drop as it reconnects; that koko web stopped is not to be said then.
  const restarting = release.upgrade.state === "running" || release.upgrade.state === "succeeded";
  const called = useNotice(caller?.deviceSession?.id);
  const notice = restarting && called === "stopped" ? undefined : called;
  const visible = routes.filter((r) => !r.administers || caller?.administers);
  const places = caller ? visible.filter((r) => r.nav) : [];
  const main = useRef<HTMLElement>(null);
  useFocusMainAfterVisit(main);
  const { pathname } = useLocation();
  const onTerminal = pathname === terminalPath;
  // A list page fills the room beside the rail; any other, and a path with no page, keeps the column.
  const fill = matchRoute(visible, pathname)?.route.fill === true;
  // On the terminal a phone's frame, and a phone's held sideways, gives way to the workspace's own phone layout.
  const bare = onTerminal && (phone || sideways);
  const { loaded: terminalLoaded } = useWorkspace();
  useTerminalKeys(onTerminal);
  // The terminal workspace lives as long as the frame: a document's, or until signing out.
  useEffect(() => {
    startWorkspace(window.location.pathname, window.location.search);
    return resetWorkspace;
  }, []);

  const skip = (event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault();
    // The page takes no focus while the rail lies over it: the rail goes first, at once.
    if (rail.state === "over") {
      flushSync(rail.dismiss);
    }
    main.current?.focus();
  };

  return (
    <CallerContext value={caller}>
      <ReleaseProvider value={release}>
        <div className="min-h-dvh" data-rail={phone || bare ? "none" : rail.state === "open" ? "open" : "folded"}>
          <a
            href="#main"
            onClick={skip}
            className="sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-4 focus:z-[60] focus:rounded-full focus:bg-surface focus:px-4 focus:py-2 focus:font-medium focus:shadow-sheet"
          >
            {t("shell.skip")}
          </a>
          {bare ? null : phone ? <PhoneTop caller={caller} /> : <Rail places={places} caller={caller} rail={rail} />}
          {/* The room beside the rail; while the rail lies over it, the page takes no input. */}
          <div className="ps-[var(--rail-w)]" inert={rail.state === "over"}>
            {notice && !onTerminal && <NoticeBar notice={notice} />}
            <main
              id="main"
              ref={main}
              tabIndex={-1}
              className={
                onTerminal ? "outline-none" : `w-full px-4 pb-28 outline-none md:px-8 md:pb-16 ${fill ? "" : "max-w-[76rem]"}`
              }
            >
              {caller?.administers && !onTerminal && <ReleaseNotice />}
              {caller ? (
                <Page routes={visible} places={places} />
              ) : notice ? null : userCurrent.error !== undefined ? (
                <div className="pt-6">
                  <LoadFailed error={userCurrent.error} retry={userCurrent.reload} />
                </div>
              ) : (
                <Loading />
              )}
              {/* The terminal workspace: kept from the first terminal on, shown on its page alone, which its notice heads. */}
              {caller && terminalLoaded && (
                <WorkspaceLayer
                  shown={onTerminal}
                  notice={onTerminal && notice ? <NoticeBar notice={notice} /> : null}
                  places={places}
                />
              )}
            </main>
          </div>
          {phone && !bare && places.length > 0 && <PhoneBar places={places} />}
          <CloseQuestion />
          <WorkspaceNotices />
        </div>
      </ReleaseProvider>
    </CallerContext>
  );
}

/**
 * useFocusMainAfterVisit moves the focus to the main region after a visit
 * that left it on nothing, as when the link followed went with the page it
 * was on, so reading starts again at the new page. A visit that left the
 * focus where it was, such as on a place in the rail, keeps it there.
 */
function useFocusMainAfterVisit(main: RefObject<HTMLElement | null>): void {
  const { visit } = useLocation();
  const first = useRef(visit);
  useEffect(() => {
    if (visit === first.current) {
      return;
    }
    const focused = document.activeElement;
    if (focused === null || focused === document.body) {
      main.current?.focus({ preventScroll: true });
    }
  }, [visit, main]);
}

/** notices are how each notice shows: its tone, and the messages of its title and description. */
const notices = {
  stopped: { tone: "warning", title: "shell.notice.stopped.title", description: "shell.notice.stopped.description" },
  revoked: { tone: "danger", title: "shell.notice.revoked.title", description: "shell.notice.revoked.description" },
  cookie: { tone: "warning", title: "error.cookie_required", description: undefined },
} as const satisfies Record<
  Notice,
  { tone: "warning" | "danger"; title: MessageKey; description: MessageKey | undefined }
>;

/**
 * NoticeBar says why the web UI cannot act, in one notice across the room
 * beside the rail, at the head of the page's column, announced as an alert,
 * until what ends it. It is not a toast.
 */
function NoticeBar({ notice }: { notice: Notice }) {
  const t = useT();
  const { tone, title, description } = notices[notice];
  const Icon = tone === "danger" ? CircleAlert : TriangleAlert;
  return (
    <div
      role="alert"
      className={tone === "danger" ? "bg-danger-soft text-danger-soft-foreground" : "bg-warning-soft text-warning-soft-foreground"}
    >
      <div className="flex items-start gap-3 px-4 py-3 md:px-8">
        <Icon aria-hidden className="mt-0.5 size-5 shrink-0" />
        <p>
          <span className="font-semibold">{t(title)}</span>
          {description !== undefined && <span> {t(description)}</span>}
        </p>
      </div>
    </div>
  );
}

/**
 * Page renders the page the URL names among the routes the caller may see;
 * / goes, keeping its query, to the first place. A path no route matches,
 * or one only the Owner may open, to a Member, shows "Page not found".
 */
function Page({ routes, places }: { routes: readonly Route[]; places: readonly Route[] }) {
  const { pathname, search } = useLocation();
  const [first] = places;
  const home = pathname === "/" ? first : undefined;
  useEffect(() => {
    if (home) {
      navigate(home.path + search, { replace: true });
    }
  }, [home, search]);
  if (home) {
    return null;
  }
  const match = matchRoute(routes, pathname);
  if (!match) {
    return <NotFound first={first} />;
  }
  const { page: PageComponent } = match.route;
  return <PageComponent params={match.params} />;
}

/** NotFound is the frame, a title and a link to the first place, when there is one. */
function NotFound({ first }: { first: Route | undefined }) {
  const t = useT();
  return (
    <PageHead title={t("shell.notFound.title")}>
      {first?.nav && (
        <Link href={first.path} className={buttonVariants({ variant: "secondary" })}>
          {t("shell.notFound.home", { page: t(first.nav) })}
        </Link>
      )}
    </PageHead>
  );
}
