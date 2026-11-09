// A stand-in for window.matchMedia, which jsdom lacks and Astryx reads: the
// system's colour scheme is light and the viewport a desktop's, 1280 CSS
// pixels wide, unless a test says otherwise. A query of the width in the
// forms Astryx asks it, "(width < 768px)" and "(max-width: 640px)", follows
// the width; every other media query matches nothing.

type Scheme = "light" | "dark";

/** desktopWidth is the viewport's width unless a test makes it a phone's. */
const desktopWidth = 1280;

let scheme: Scheme = "light";
let width = desktopWidth;
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
    if (asked !== undefined) {
      return asked === scheme;
    }
    const narrowerThan = /^\(width < (\d+)px\)$/.exec(this.media)?.[1];
    if (narrowerThan !== undefined) {
      return width < Number(narrowerThan);
    }
    const atMost = /^\(max-width: (\d+)px\)$/.exec(this.media)?.[1];
    return atMost !== undefined && width <= Number(atMost);
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
  changeMedia(() => {
    scheme = next;
  });
}

/**
 * setViewportWidth makes the viewport this many CSS pixels wide, telling
 * every query of the width that changed, as a browser does when its window
 * is resized. Set it before rendering to render as on a phone.
 */
export function setViewportWidth(next: number): void {
  changeMedia(() => {
    width = next;
  });
}

/** changeMedia makes a change to the media, then tells every query whose answer it changed. */
function changeMedia(change: () => void): void {
  const before = new Map([...lists].map((list) => [list, list.matches]));
  change();
  for (const [list, matched] of before) {
    if (list.matches !== matched) {
      const event = Object.assign(new Event("change"), { matches: list.matches, media: list.media });
      list.dispatchEvent(event);
      list.onchange?.call(list, event as MediaQueryListEvent);
    }
  }
}

/** resetMedia puts the system back in light mode and the viewport back to a desktop's, and forgets every query. */
export function resetMedia(): void {
  scheme = "light";
  width = desktopWidth;
  lists.clear();
}
