import { useEffect } from "react";

import { UserCurrent } from "../api/user";
import { useRead } from "./data";
import { ErrorMessage, LanguageSwitch, useT } from "./i18n";
import { type Caller, CallerContext, callerOf } from "./caller";
import { type Notice, useNotice } from "./notice";
import { Providers } from "./providers";
import { Link, matchRoute, navigate, type Route, useLocation } from "./router";

/**
 * Shell is the frame every page sits in, inside the Providers: the header
 * with the User, the device name, the links to the pages and the language
 * switch; the notices; and the page the URL names. It learns who the web UI
 * acts as from user.current, reloaded as every read is, and renders a page
 * only once it knows, so a Member never sees an Owner-only page or button.
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
      <header className="shell-header koko-legacy">
        <strong className="brand">{t("shell.brand")}</strong>
        {caller && (
          <nav aria-label={t("shell.nav")}>
            {visible.flatMap((r) =>
              r.nav ? [
                <Link key={r.path} to={r.path}>
                  {t(r.nav)}
                </Link>,
              ] : [],
            )}
          </nav>
        )}
        {caller && <Who caller={caller} />}
        <LanguageSwitch />
      </header>
      {notice && <NoticeBanner notice={notice} />}
      <main className="koko-legacy">
        {caller ? (
          <Page routes={visible} />
        ) : notice ? null : userCurrent.error ? (
          <ErrorMessage error={userCurrent.error} />
        ) : (
          <p>{t("shell.loading")}</p>
        )}
      </main>
    </CallerContext>
  );
}

/** Who says whom the web UI acts as, and from which device. */
function Who({ caller }: { caller: Caller }) {
  const t = useT();
  const user = caller.user.name;
  return (
    <span className="who">
      {caller.deviceSession
        ? t("shell.header.device", { user, device: caller.deviceSession.device })
        : t("shell.header.local", { user })}
    </span>
  );
}

function NoticeBanner({ notice }: { notice: Notice }) {
  const t = useT();
  const message = {
    stopped: t("shell.notice.stopped"),
    revoked: t("shell.notice.revoked"),
    cookie: t("error.cookie_required"),
  }[notice];
  return (
    <div className="notice" role="alert">
      {message}
    </div>
  );
}

/**
 * Page renders the page the URL names among the routes the caller may see;
 * / goes to the first page the header links.
 */
function Page({ routes }: { routes: readonly Route[] }) {
  const t = useT();
  const { pathname, search } = useLocation();
  const home = pathname === "/" ? routes.find((r) => r.nav) : undefined;
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
    return <p>{t("shell.notFound")}</p>;
  }
  const { page: PageComponent } = match.route;
  return <PageComponent params={match.params} />;
}
