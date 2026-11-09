import { Banner } from "@astryxdesign/core/Banner";
import { Spinner } from "@astryxdesign/core/Spinner";
import { Component, type ComponentType, lazy, type ReactNode, Suspense } from "react";

import { useT } from "./i18n";
import type { PageProps } from "./router";

/**
 * lazyPage is a page whose code loads, as a chunk of its own, the first
 * time the web UI shows it, so the first load carries only the shell and
 * the page the browser opens (the size budget, docs/subsystems/web-ui.md).
 * While the chunk loads the page shows a spinner; a chunk that does not
 * load, as after the koko server was upgraded under an open tab, says so in
 * place instead of leaving the frame empty.
 */
export function lazyPage(load: () => Promise<ComponentType<PageProps>>): ComponentType<PageProps> {
  const Page = lazy(async () => {
    try {
      return { default: await load() };
    } catch (cause) {
      throw new ChunkNotLoaded(cause);
    }
  });
  return function LazyPage(props: PageProps) {
    return (
      <LoadFailure>
        <Suspense fallback={<Loading />}>
          <Page {...props} />
        </Suspense>
      </LoadFailure>
    );
  };
}

function Loading() {
  const t = useT();
  return <Spinner label={t("shell.loading")} />;
}

/** ChunkNotLoaded is why a page's chunk did not load. */
class ChunkNotLoaded extends Error {
  constructor(cause: unknown) {
    super("the page's chunk did not load", { cause });
  }
}

/**
 * LoadFailure shows that a page's chunk did not load, in place of the page.
 * Any other error the page throws goes on to the boundary above, since it
 * is no failure to load.
 */
class LoadFailure extends Component<{ children: ReactNode }, { error?: unknown }> {
  override state: { error?: unknown } = {};

  static getDerivedStateFromError(error: unknown): { error: unknown } {
    return { error };
  }

  override render() {
    const { error } = this.state;
    if (error === undefined) {
      return this.props.children;
    }
    if (error instanceof ChunkNotLoaded) {
      return <LoadFailed />;
    }
    throw error;
  }
}

function LoadFailed() {
  const t = useT();
  return <Banner status="error" role="alert" title={t("shell.pageNotLoaded")} />;
}
