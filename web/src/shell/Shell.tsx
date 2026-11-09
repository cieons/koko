import { AppShell, useAppShellMobile } from "@astryxdesign/core/AppShell";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { NavIcon } from "@astryxdesign/core/NavIcon";
import { SideNav, SideNavItem, SideNavSection } from "@astryxdesign/core/SideNav";
import { Spinner } from "@astryxdesign/core/Spinner";
import { TopNav, TopNavHeading } from "@astryxdesign/core/TopNav";
import { Server, SearchX } from "lucide-react";
import { useEffect, useEffectEvent } from "react";

import { UserCurrent } from "../api/user";
import type { MessageKey } from "../i18n/catalogues";
import { AccountMenu } from "./AccountMenu";
import { CallerContext, callerOf } from "./caller";
import { useRead } from "./data";
import { ErrorMessage, useT } from "./i18n";
import { type Notice, useNotice } from "./notice";
import { Providers } from "./providers";
import { matchRoute, navigate, type Route, useLocation } from "./router";

/**
 * Shell is the frame every page sits in, inside the Providers: Astryx's
 * AppShell with a top bar (koko's name, whom the web UI acts as, and the
 * account menu), the side navigation to the pages, a MobileNav drawer in its
 * place on a phone, the notices above them, and the page the URL names. It
 * learns who the web UI acts as from user.current, reloaded as every read
 * is, and renders a page only once it knows, so a Member never sees an
 * Owner-only page or link.
 */
export function Shell({ routes }: { routes: readonly Route[] }) {
  return (
    <Providers>
      <Frame routes={routes} />
    </Providers>
  );
}

function Frame({ routes }: { routes: readonly Route[] }) {
  const t = useT();
  const userCurrent = useRead(UserCurrent, {});
  const caller = userCurrent.data && callerOf(userCurrent.data);
  const notice = useNotice(caller?.deviceSession?.id);
  const visible = routes.filter((r) => !r.ownerOnly || caller?.owner);

  return (
    <CallerContext value={caller}>
      <AppShell
        contentPadding={6}
        banner={notice && <NoticeBanner notice={notice} />}
        topNav={
          <TopNav
            label={t("shell.brand")}
            heading={
              <TopNavHeading heading={t("shell.brand")} headingHref="/" logo={<NavIcon icon={<Server />} />} />
            }
            endContent={<AccountMenu caller={caller} />}
          />
        }
        sideNav={caller && <PageNav routes={visible} />}
      >
        <CloseMobileNavOnVisit />
        {caller ? (
          <Page routes={visible} />
        ) : notice ? null : userCurrent.error ? (
          <ErrorMessage error={userCurrent.error} />
        ) : (
          <Spinner label={t("shell.loading")} />
        )}
      </AppShell>
    </CallerContext>
  );
}

/** PageNav links each page the caller may see, in the order of routes, and marks the one the URL is in. */
function PageNav({ routes }: { routes: readonly Route[] }) {
  const t = useT();
  const { pathname } = useLocation();
  return (
    <SideNav aria-label={t("shell.nav")}>
      <SideNavSection title={t("shell.nav")} isHeaderHidden>
        {routes.flatMap((r) =>
          r.nav
            ? [
                <SideNavItem
                  key={r.path}
                  label={t(r.nav)}
                  href={r.path}
                  icon={r.icon}
                  isSelected={pathname === r.path || pathname.startsWith(`${r.path}/`)}
                />,
              ]
            : [],
        )}
      </SideNavSection>
    </SideNav>
  );
}

/** CloseMobileNavOnVisit closes the MobileNav drawer on every visit, such as one its links made. */
function CloseMobileNavOnVisit() {
  const { visit } = useLocation();
  const { closeMobileNav } = useAppShellMobile();
  // closeMobileNav changes as the drawer opens, which is no visit.
  const close = useEffectEvent(() => closeMobileNav());
  useEffect(() => close(), [visit]);
  return null;
}

/** notices are how each notice shows: its status, and the messages of its title and description. */
const notices = {
  stopped: {
    status: "warning",
    title: "shell.notice.stopped.title",
    description: "shell.notice.stopped.description",
  },
  revoked: {
    status: "error",
    title: "shell.notice.revoked.title",
    description: "shell.notice.revoked.description",
  },
  cookie: { status: "warning", title: "error.cookie_required", description: undefined },
} as const satisfies Record<Notice, { status: string; title: MessageKey; description: MessageKey | undefined }>;

/** NoticeBanner says, above the frame, why the web UI cannot act, announced as an alert. */
function NoticeBanner({ notice }: { notice: Notice }) {
  const t = useT();
  const { status, title, description } = notices[notice];
  return (
    <Banner
      status={status}
      container="section"
      role="alert"
      title={t(title)}
      description={description && t(description)}
    />
  );
}

/**
 * Page renders the page the URL names among the routes the caller may see;
 * / goes to the first page the side navigation links.
 */
function Page({ routes }: { routes: readonly Route[] }) {
  const t = useT();
  const { pathname, search } = useLocation();
  const first = routes.find((r) => r.nav);
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
    return (
      <EmptyState
        headingLevel={1}
        icon={<SearchX />}
        title={t("shell.notFound.title")}
        description={t("shell.notFound.description")}
        actions={first && <Button variant="secondary" label={t("shell.notFound.home")} href={first.path} />}
      />
    );
  }
  const { page: PageComponent } = match.route;
  return (
    // .koko-page is the element a page's own CSS may size, such as the
    // Hosts list, which fills the main area (host.css).
    <div className="koko-page">
      <PageComponent params={match.params} />
    </div>
  );
}
