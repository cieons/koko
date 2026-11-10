import { type ReactNode, useEffect, useId } from "react";

import { useT } from "../shell/i18n";

/**
 * useDocumentTitle names the browser's tab, and what a screen reader reads
 * of it, after the page: its own name, then koko ("web1 · koko").
 */
export function useDocumentTitle(name: string): void {
  const t = useT();
  const title = t("shell.documentTitle", { page: name });
  const brand = t("shell.brand");
  useEffect(() => {
    document.title = title;
    return () => {
      document.title = brand;
    };
  }, [title, brand]);
}

/** PageProps are what a page's head shows. */
export interface PageProps {
  /** title is the page's title, such as "Hosts" or a Host's name. */
  title: ReactNode;
  /** name names the browser's tab where the title is more than text, such as a name beside its state mark. */
  name?: string;
  /** lead is the optional line under the title. */
  lead?: ReactNode;
  /** actions are the page's one or two actions, to the right of the title. */
  actions?: ReactNode;
  /** breadcrumb, on a detail page, is the way back above the title. */
  breadcrumb?: ReactNode;
  children: ReactNode;
}

/**
 * Page is a page's head and content (docs/web-design.md, "The frame"): a
 * title in the display face, an optional line under it, and its actions to
 * the right; a detail page has its breadcrumb above. It names the browser's
 * tab after the page.
 */
export function Page({ title, name, lead, actions, breadcrumb, children }: PageProps) {
  useDocumentTitle(name ?? (typeof title === "string" ? title : ""));
  return (
    <div className="pt-6 md:pt-9">
      {breadcrumb !== undefined && <div className="mb-2">{breadcrumb}</div>}
      <div className="mb-6 flex flex-wrap items-end justify-between gap-x-6 gap-y-4">
        <div className="min-w-0">
          <h1 className="t-display">{title}</h1>
          {lead !== undefined && <p className="mt-1 max-w-[68ch] text-muted">{lead}</p>}
        </div>
        {actions !== undefined && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children}
    </div>
  );
}

/** SheetProps are what a panel holds. */
export interface SheetProps {
  /** title, when given, heads the panel and names it as a region. */
  title?: string;
  /** action is a button beside the title, such as Add Service. */
  action?: ReactNode;
  /** list lays a list on the panel: no padding, so its rows run edge to edge with hairlines between them. */
  list?: boolean;
  className?: string;
  children: ReactNode;
}

/**
 * Sheet is a panel (docs/web-design.md, "Components"): the one raised
 * surface every group of content lies on.
 */
export function Sheet({ title, action, list = false, className = "", children }: SheetProps) {
  const id = useId();
  return (
    <section
      aria-labelledby={title === undefined ? undefined : id}
      className={`sheet ${list ? "overflow-hidden" : "p-6"} ${className}`}
    >
      {title !== undefined && (
        <div
          className={`flex min-h-10 items-center justify-between gap-3 ${list ? "border-b border-line px-6 py-3" : "mb-4"}`}
        >
          <h2 id={id} className="t-heading">
            {title}
          </h2>
          {action}
        </div>
      )}
      {children}
    </section>
  );
}
