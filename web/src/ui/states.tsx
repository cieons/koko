import { Button, Skeleton } from "@heroui/react";
import { CircleAlert } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";

import { CodeSignInRequired } from "../api/web";
import { ApiError } from "../client";
import type { Read } from "../shell/data";
import { errorMessage, useT } from "../shell/i18n";
import { navigate, signInPage, signInPath, useLocation } from "../shell/router";
import { Sheet } from "./Page";

// The states every screen has (docs/web-design.md, "States every screen
// has"): loading, failed to load, empty, no match and a refused write.

/** loadingDelay is how long a first call runs before its panel shows skeleton rows. */
export const loadingDelay = 300;

/**
 * Loading stands in a panel for its list while the first call runs: nothing
 * for its first 300 ms, so a quick answer shows no flash, then three
 * skeleton rows, announced as loading.
 */
export function Loading() {
  const t = useT();
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const timer = setTimeout(() => setShown(true), loadingDelay);
    return () => clearTimeout(timer);
  }, []);
  if (!shown) {
    return null;
  }
  return (
    <div role="status" className="flex flex-col gap-3 px-6 py-5">
      <span className="sr-only">{t("shell.loading")}</span>
      {[0, 1, 2].map((row) => (
        <div key={row} aria-hidden className="flex items-center gap-4">
          <Skeleton className="h-4 w-1/4 rounded-md" />
          <Skeleton className="h-4 flex-1 rounded-md" />
        </div>
      ))}
    </div>
  );
}

/**
 * LoadFailed says in its panel why a read failed, in the koko server's
 * message, with Try again. A read refused signin_required, from a browser
 * not signed in on the web listener, goes to Sign in instead, which comes
 * back to this page after.
 */
export function LoadFailed({ error, retry }: { error: unknown; retry: () => void }) {
  const t = useT();
  const { pathname, search } = useLocation();
  // Sign in is the one page that never goes to itself.
  const signin = error instanceof ApiError && error.code === CodeSignInRequired && pathname !== signInPage;
  useEffect(() => {
    if (signin) {
      navigate(signInPath(pathname + search), { replace: true });
    }
  }, [signin, pathname, search]);
  if (signin) {
    return null;
  }
  return (
    <div className="flex flex-col items-start gap-3 px-6 py-5">
      <Refusal error={error} />
      <Button variant="secondary" onPress={retry}>
        {t("shell.retry")}
      </Button>
    </div>
  );
}

/**
 * Loaded shows a read's answer through children once it has one; until
 * then, Loading; and when it failed, LoadFailed, above the last answer if a
 * reload failed, so what was shown stays.
 */
export function Loaded<T>({ read, children }: { read: Read<T>; children: (data: T) => ReactNode }) {
  const { data, error, reload } = read;
  return (
    <>
      {error !== undefined && <LoadFailed error={error} retry={reload} />}
      {data !== undefined ? children(data) : error === undefined && <Loading />}
    </>
  );
}

/** EmptyStateProps are what a screen says when it has nothing to list yet. */
export interface EmptyStateProps {
  /** title says what is missing, such as "No Host is recorded yet". */
  title: string;
  /** lead is a sentence saying what the thing is for. */
  lead: string;
  /** action adds the first one. */
  action?: ReactNode;
}

/** EmptyState is one panel: a line saying what is missing, what it is for, and the action that adds one. */
export function EmptyState({ title, lead, action }: EmptyStateProps) {
  return (
    <Sheet className="flex flex-col items-start gap-3 md:p-10">
      <h2 className="t-title">{title}</h2>
      <p className="max-w-[60ch] text-muted">{lead}</p>
      {action !== undefined && <div className="mt-2">{action}</div>}
    </Sheet>
  );
}

/** NoMatchProps are what a list says when nothing matches its view. */
export interface NoMatchProps {
  /** message says so, such as "No Host matches this view." */
  message: string;
  /** clear labels the button that clears the search and filters. */
  clear: string;
  onClear: () => void;
}

/** NoMatch is one panel saying that nothing matches the view, and the button that clears it. */
export function NoMatch({ message, clear, onClear }: NoMatchProps) {
  return (
    <Sheet className="flex flex-col items-start gap-3">
      <p className="t-name">{message}</p>
      <Button variant="secondary" onPress={onClear}>
        {clear}
      </Button>
    </Sheet>
  );
}

/**
 * AlertLine says, in place and announced as an alert, why something could
 * not be done: a refusal, or a page that did not load.
 */
export function AlertLine({ children }: { children: ReactNode }) {
  return (
    <p
      role="alert"
      className="flex items-start gap-2 rounded-2xl bg-danger-soft px-4 py-3 text-danger-soft-foreground"
    >
      <CircleAlert aria-hidden className="mt-0.5 size-5 shrink-0" />
      {children}
    </p>
  );
}

/**
 * Refusal shows why a call failed, in place, where the action was: the
 * message of the API error's code, announced as an alert, never a toast; or,
 * where the page says more than the code's message, such as the Personal
 * Space that keeps a User from being deleted, the page's own message,
 * given as a string. It shows nothing while there is no error.
 */
export function Refusal({ error }: { error: unknown }) {
  const t = useT();
  if (error === undefined) {
    return null;
  }
  return <AlertLine>{typeof error === "string" ? error : errorMessage(t, error)}</AlertLine>;
}
