// A stand-in for window.matchMedia, which jsdom lacks and Astryx reads: the
// system's colour scheme is light unless a test says otherwise, and every
// other media query matches nothing.

type Scheme = "light" | "dark";

let scheme: Scheme = "light";
const lists = new Set<FakeMediaQueryList>();

class FakeMediaQueryList extends EventTarget implements MediaQueryList {
  readonly media: string;
  onchange: ((this: MediaQueryList, event: MediaQueryListEvent) => unknown) | null = null;

  constructor(media: string) {
    super();
    this.media = media;
  }

  get matches(): boolean {
    const asked = /prefers-color-scheme:\s*(light|dark)/.exec(this.media)?.[1];
    return asked === scheme;
  }

  addListener(listener: ((this: MediaQueryList, event: MediaQueryListEvent) => unknown) | null): void {
    if (listener) {
      this.addEventListener("change", listener as EventListener);
    }
  }

  removeListener(listener: ((this: MediaQueryList, event: MediaQueryListEvent) => unknown) | null): void {
    if (listener) {
      this.removeEventListener("change", listener as EventListener);
    }
  }
}

/** installMatchMedia puts the stand-in in place of window.matchMedia. */
export function installMatchMedia(): void {
  window.matchMedia = (media: string) => {
    const list = new FakeMediaQueryList(media);
    lists.add(list);
    return list;
  };
}

/**
 * setSystemScheme makes the system's colour scheme light or dark, telling
 * every query of it that changed, as a browser does when the system's mode
 * changes.
 */
export function setSystemScheme(next: Scheme): void {
  const before = new Map([...lists].map((list) => [list, list.matches]));
  scheme = next;
  for (const [list, matched] of before) {
    if (list.matches !== matched) {
      const event = Object.assign(new Event("change"), { matches: list.matches, media: list.media });
      list.dispatchEvent(event);
      list.onchange?.call(list, event as MediaQueryListEvent);
    }
  }
}

/** resetMedia puts the system back in light mode and forgets every query. */
export function resetMedia(): void {
  scheme = "light";
  lists.clear();
}
