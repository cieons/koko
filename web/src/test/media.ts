// A stand-in for window.matchMedia, which jsdom lacks: the system's colour
// scheme is light, the viewport a desktop's, 1280 by 800 CSS pixels, and
// the pointer a mouse, unless a test says otherwise. A query of the width
// follows it, in the forms Tailwind's breakpoints and the web UI ask it:
// "(width < 768px)", "(width >= 48rem)", "(max-width: 640px)",
// "(min-width: 40rem)", a rem being 16 pixels; "(max-height: 540px)" and
// "(min-height: …)" the height, "(orientation: landscape)" or "portrait"
// the two, and "(pointer: coarse)" or "fine" the pointer. Queries joined by
// "and" match when each does. The system asks for reduced motion only
// within withoutMotion. Every other media query matches nothing.

type Scheme = "light" | "dark";

/** desktopWidth and desktopHeight are the viewport's size unless a test makes it a phone's. */
const desktopWidth = 1280;
const desktopHeight = 800;

let scheme: Scheme = "light";
let width = desktopWidth;
let height = desktopHeight;
let coarse = false;
let reducedMotion = false;
const lists = new Set<FakeMediaQueryList>();

/** pixels is a length of a media query in CSS pixels. */
function pixels(amount: string, unit: string): number {
  return Number(amount) * (unit === "rem" || unit === "em" ? 16 : 1);
}

/** featureMatches answers one feature of the viewport: its width, height, orientation or pointer; or undefined for any other. */
function featureMatches(media: string): boolean | undefined {
  const range = /^\(width\s*(<|<=|>|>=)\s*([\d.]+)(px|rem|em)\)$/.exec(media);
  if (range) {
    const [, op, amount = "", unit = ""] = range;
    const edge = pixels(amount, unit);
    return { "<": width < edge, "<=": width <= edge, ">": width > edge, ">=": width >= edge }[op as "<"];
  }
  const bound = /^\((min|max)-(width|height):\s*([\d.]+)(px|rem|em)\)$/.exec(media);
  if (bound) {
    const [, side, dimension, amount = "", unit = ""] = bound;
    const edge = pixels(amount, unit);
    const size = dimension === "width" ? width : height;
    return side === "min" ? size >= edge : size <= edge;
  }
  const orientation = /^\(orientation:\s*(landscape|portrait)\)$/.exec(media);
  if (orientation) {
    return (orientation[1] === "landscape") === width > height;
  }
  const pointer = /^\(pointer:\s*(coarse|fine)\)$/.exec(media);
  if (pointer) {
    return (pointer[1] === "coarse") === coarse;
  }
  return undefined;
}

/** viewportMatches answers a query of the viewport, its parts joined by "and" each answered by featureMatches. */
function viewportMatches(media: string): boolean | undefined {
  const answers = media.split(/\s+and\s+/).map((part) => featureMatches(part.trim()));
  return answers.includes(undefined) ? undefined : answers.every(Boolean);
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
    return viewportMatches(this.media.trim()) ?? false;
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
 * setViewport makes the viewport this many CSS pixels wide and high, and its
 * primary pointer a touch screen (coarse) or not, telling every query that
 * changed, as a phone turning does. Set it before rendering to render as on
 * a phone.
 */
export function setViewport(next: { width: number; height: number; coarse: boolean }): void {
  changeMedia(() => {
    width = next.width;
    height = next.height;
    coarse = next.coarse;
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
  height = desktopHeight;
  coarse = false;
  lists.clear();
}
