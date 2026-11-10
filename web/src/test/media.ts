// A stand-in for window.matchMedia, which jsdom lacks: the system's colour
// scheme is light and the viewport a desktop's, 1280 CSS pixels wide, unless
// a test says otherwise. A query of the width follows it, in the forms
// Tailwind's breakpoints and the web UI ask it: "(width < 768px)",
// "(width >= 48rem)", "(max-width: 640px)", "(min-width: 40rem)", a rem
// being 16 pixels. The system asks for reduced motion only within
// withoutMotion. Every other media query matches nothing.

type Scheme = "light" | "dark";

/** desktopWidth is the viewport's width unless a test makes it a phone's. */
const desktopWidth = 1280;

let scheme: Scheme = "light";
let width = desktopWidth;
let reducedMotion = false;
const lists = new Set<FakeMediaQueryList>();

/** pixels is a length of a media query in CSS pixels. */
function pixels(amount: string, unit: string): number {
  return Number(amount) * (unit === "rem" || unit === "em" ? 16 : 1);
}

/** widthMatches answers a query of the viewport's width, or undefined for any other query. */
function widthMatches(media: string): boolean | undefined {
  const range = /^\(width\s*(<|<=|>|>=)\s*([\d.]+)(px|rem|em)\)$/.exec(media);
  if (range) {
    const [, op, amount = "", unit = ""] = range;
    const edge = pixels(amount, unit);
    return { "<": width < edge, "<=": width <= edge, ">": width > edge, ">=": width >= edge }[op as "<"];
  }
  const bound = /^\((min|max)-width:\s*([\d.]+)(px|rem|em)\)$/.exec(media);
  if (bound) {
    const [, side, amount = "", unit = ""] = bound;
    const edge = pixels(amount, unit);
    return side === "min" ? width >= edge : width <= edge;
  }
  return undefined;
}

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
    if (/prefers-reduced-motion:\s*reduce/.test(this.media)) {
      return reducedMotion;
    }
    return widthMatches(this.media.trim()) ?? false;
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

/**
 * withoutMotion runs fn as on a system that asks for reduced motion, such
 * as closing toasts, which HeroUI then removes at once rather than after
 * their exit animation.
 */
export function withoutMotion(fn: () => void): void {
  reducedMotion = true;
  try {
    fn();
  } finally {
    reducedMotion = false;
  }
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
