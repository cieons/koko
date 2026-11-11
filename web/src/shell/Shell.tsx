import { buttonVariants } from "@heroui/react";
import { CircleAlert, TriangleAlert } from "lucide-react";
import { type MouseEvent, type RefObject, useEffect, useRef } from "react";
import { Link } from "react-aria-components";

import { UserCurrent } from "../api/user";
import { CodeSignInRequired } from "../api/web";
import { ApiError, observeCalls } from "../client";
import type { MessageKey } from "../i18n/catalogues";
import { Page as PageHead } from "../ui/Page";
import { usePhone } from "../ui/phone";
import { Loading, LoadFailed } from "../ui/states";
import { CallerContext, callerOf } from "./caller";
import { useRead } from "./data";
import { useT } from "./i18n";
import { type Notice, useNotice } from "./notice";
import { Providers } from "./providers";
import { ReleaseNotice, ReleaseProvider, useReleaseState } from "./release";
import { matchRoute, navigate, type Route, signInPage, signInPath, useLocation } from "./router";
import { AccountPanel, LanguageChoice, ModeChoice } from "./TopBar";

/**
 * Shell is the frame every page sits in, inside the Providers
 * (docs/web-design.md, "The frame"): a skip link; the sticky top bar with
 * koko's wordmark, the places the caller may see and, at its end, Language,
 * Appearance and the account; the shell's notice under it; in the main
 * region, for the Owner, the notice of a newer koko release, then the page
 * the URL names; and on a phone the places in a bar at the foot.
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
  const userCurrent = useRead(UserCurrent, {});
  const caller = userCurrent.data && callerOf(userCurrent.data);
  const release = useReleaseState(caller?.owner === true);
  // While the koko server restarts for an upgrade, calls through koko web
  // drop as it reconnects; that koko web stopped is not to be said then.
  const restarting = release.upgrade.state === "running" || release.upgrade.state === "succeeded";
  const called = useNotice(caller?.deviceSession?.id);
  const notice = restarting && called === "stopped" ? undefined : called;
  const visible = routes.filter((r) => !r.ownerOnly || caller?.owner);
  const places = caller ? visible.filter((r) => r.nav) : [];
  const main = useRef<HTMLElement>(null);
  useFocusMainAfterVisit(main);

  const skip = (event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault();
    main.current?.focus();
  };

  return (
    <CallerContext value={caller}>
      <ReleaseProvider value={release}>
        <div className="flex min-h-dvh flex-col">
          <a
            href="#main"
            onClick={skip}
            className="sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-4 focus:z-50 focus:rounded-full focus:bg-surface focus:px-4 focus:py-2 focus:font-medium focus:shadow-sheet"
          >
            {t("shell.skip")}
          </a>
          <header className="sticky top-0 z-20 border-b border-line bg-background/80 backdrop-blur-xl">
            <div className="mx-auto grid h-16 max-w-[76rem] grid-cols-[1fr_auto_1fr] items-center gap-4 px-4 md:px-8">
              <div className="flex min-w-0 items-center">
                <Link
                  href="/"
                  className="rounded-md text-xl leading-none font-semibold tracking-[-0.04em] text-foreground outline-none select-none data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus"
                >
                  {t("shell.brand")}
                </Link>
              </div>
              {!phone && places.length > 0 && (
                <Places
                  places={places}
                  className="flex items-center gap-0.5 rounded-full bg-surface p-1 shadow-sheet"
                  item="h-8 px-4"
                />
              )}
              <div className="col-start-3 flex items-center justify-end gap-0.5">
                <LanguageChoice />
                <ModeChoice />
                {caller && <AccountPanel caller={caller} />}
              </div>
            </div>
          </header>
          {notice && <NoticeBar notice={notice} />}
          <main
            id="main"
            ref={main}
            tabIndex={-1}
            className="mx-auto w-full max-w-[76rem] flex-1 px-4 pb-28 outline-none md:px-8 md:pb-16"
          >
            {caller?.owner && <ReleaseNotice />}
            {caller ? (
              <Page routes={visible} places={places} />
            ) : notice ? null : userCurrent.error !== undefined ? (
              <div className="pt-6">
                <LoadFailed error={userCurrent.error} retry={userCurrent.reload} />
              </div>
            ) : (
              <Loading />
            )}
          </main>
          {phone && places.length > 0 && (
            <div className="fixed inset-x-0 bottom-0 z-30 flex justify-center px-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
              <Places
                places={places}
                className="flex w-full max-w-md items-stretch gap-1 rounded-full bg-surface p-1 shadow-sheet"
                item="min-h-11 flex-1"
                icons
              />
            </div>
          )}
        </div>
      </ReleaseProvider>
    </CallerContext>
  );
}

/**
 * useFocusMainAfterVisit moves the focus to the main region after a visit
 * that left it on nothing, as when the link followed went with the page it
 * was on, so reading starts again at the new page. A visit that left the
 * focus where it was, such as on a place in the top bar, keeps it there.
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

/**
 * Places links each place the caller may see, in the order of routes, and
 * marks the one the URL is in (aria-current), a page beneath it such as a
 * Host's included: in the top bar's raised track on a desktop, and in the
 * bar at the foot, with icons, on a phone.
 */
function Places({
  places,
  className,
  item,
  icons = false,
}: {
  places: readonly Route[];
  className: string;
  item: string;
  icons?: boolean;
}) {
  const t = useT();
  const { pathname } = useLocation();
  return (
    <nav aria-label={t("shell.nav")} className={className}>
      {places.map((r) => {
        const Icon = r.icon;
        const current = pathname === r.path || pathname.startsWith(`${r.path}/`);
        return (
          <Link
            key={r.path}
            href={r.path}
            aria-current={current ? "page" : undefined}
            className={`place flex items-center justify-center gap-2 rounded-full text-sm font-medium text-muted no-underline outline-none transition-colors hover:text-foreground data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus ${item}`}
          >
            {icons && Icon && <Icon aria-hidden className="size-4" />}
            {t(r.nav as MessageKey)}
          </Link>
        );
      })}
    </nav>
  );
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
 * NoticeBar says why the web UI cannot act, in one notice across the width
 * under the top bar, announced as an alert, until what ends it. It is not a
 * toast.
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
      <div className="mx-auto flex max-w-[76rem] items-start gap-3 px-4 py-3 md:px-8">
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

/** NotFound is the frame, a title, one sentence and a link to the first place, when there is one. */
function NotFound({ first }: { first: Route | undefined }) {
  const t = useT();
  return (
    <PageHead title={t("shell.notFound.title")} lead={t("shell.notFound.description")}>
      {first?.nav && (
        <Link href={first.path} className={buttonVariants({ variant: "secondary" })}>
          {t("shell.notFound.home", { page: t(first.nav) })}
        </Link>
      )}
    </PageHead>
  );
}
