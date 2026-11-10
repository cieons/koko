import { Component, type ComponentType, lazy, type ReactNode, Suspense } from "react";

import { AlertLine, Loading } from "../ui/states";
import { useT } from "./i18n";
import type { PageProps } from "./router";

/** loaders holds, for each lazy page, the function that loads its chunk, once. */
const loaders = new WeakMap<ComponentType<PageProps>, () => Promise<unknown>>();

/**
 * lazyPage is a page whose code loads, as a chunk of its own, the first
 * time the web UI shows it, so the first load carries only the shell and
 * the page the browser opens (the size budget, docs/subsystems/web-ui.md).
 * While the chunk loads the frame shows Loading; a chunk that does not
 * load, as after the koko server was upgraded under an open tab, says so in
 * place instead of leaving the frame empty. loadPage loads it ahead of time.
 */
export function lazyPage(load: () => Promise<ComponentType<PageProps>>): ComponentType<PageProps> {
  let loading: Promise<ComponentType<PageProps>> | undefined;
  const loadOnce = () => (loading ??= load());
  const Page = lazy(async () => {
    try {
      return { default: await loadOnce() };
    } catch (cause) {
      throw new ChunkNotLoaded(cause);
    }
  });
  function LazyPage(props: PageProps) {
    return (
      <LoadFailure>
        <Suspense fallback={<Loading />}>
          <Page {...props} />
        </Suspense>
      </LoadFailure>
    );
  }
  loaders.set(LazyPage, loadOnce);
  return LazyPage;
}

/**
 * loadPage loads a lazyPage's chunk ahead of time, once, so the page's
 * first render does not wait on its chunk; for any other page it does
 * nothing. Page tests load every page this way before they run (test/pages.tsx).
 */
export async function loadPage(page: ComponentType<PageProps>): Promise<void> {
  await loaders.get(page)?.();
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
      return <ChunkFailed />;
    }
    throw error;
  }
}

/** ChunkFailed says, in place of the page, that the browser page must be reloaded. */
function ChunkFailed() {
  const t = useT();
  return (
    <div className="mt-6 md:mt-9">
      <AlertLine>{t("shell.pageNotLoaded")}</AlertLine>
    </div>
  );
}
