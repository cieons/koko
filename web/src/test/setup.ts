import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, expect, vi } from "vitest";

import indexHTML from "../../index.html?raw";
import { installCspWatch, takeCspViolations } from "./csp";
import { installMatchMedia, resetMedia, withoutMotion } from "./media";
import { FakeWebSocket } from "./terminal";

// The document's language and head are index.html's: its title, and the
// <meta> whose id keeps React Aria's usePress from adding a <style>
// element, which the CSP refuses (static.test.ts checks index.html holds it).
const index = new DOMParser().parseFromString(indexHTML, "text/html");
const head = index.head;
document.documentElement.lang = index.documentElement.lang;
document.title = head.querySelector("title")?.textContent ?? "";
const pressable = head.querySelector("#react-aria-pressable-style");
if (pressable) {
  document.head.append(document.importNode(pressable, true));
}

// Every test fails on a CSP violation, as the real browser would report one
// (test/csp.ts).
installCspWatch();

// React Aria and HeroUI read what jsdom lacks: media queries (the colour
// scheme, a phone's width), and the size, position and visibility of
// elements, which jsdom never lays out, so its observers here observe
// nothing and scrolling an element into view does nothing.
installMatchMedia();

class NoResizeObserver implements ResizeObserver {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
window.ResizeObserver ??= NoResizeObserver;

class NoIntersectionObserver implements IntersectionObserver {
  readonly root = null;
  readonly rootMargin = "0px";
  readonly scrollMargin = "0px";
  readonly thresholds = [0];
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }
}
window.IntersectionObserver ??= NoIntersectionObserver;

Element.prototype.scrollIntoView ??= function scrollIntoView() {};

// React Aria's shared element transition, behind HeroUI's tab indicator,
// asks an element for its animations, which jsdom never runs.
Element.prototype.getAnimations ??= function getAnimations() {
  return [];
};

// Every browser the web UI supports has inert, which React Aria sets on the
// page behind an open overlay; without it, React Aria falls back on
// aria-hidden, which axe rightly faults over the focusable elements inside.
// Here it reflects the attribute, as a browser's does, for axe to read.
if (!("inert" in HTMLElement.prototype)) {
  Object.defineProperty(HTMLElement.prototype, "inert", {
    configurable: true,
    get(this: HTMLElement) {
      return this.hasAttribute("inert");
    },
    set(this: HTMLElement, value: boolean) {
      this.toggleAttribute("inert", value);
    },
  });
}

// Without it axe-core stands one in that adds a <style> element, which the
// CSP watch would take for the page's.
document.elementsFromPoint ??= () => [];

// Each test starts from an empty document, at /, with no toast left from
// the one before, nothing the browser remembers, index.html's language and
// title, the system in light mode and a desktop's viewport; and ends with
// no CSP violation.
// The frame keeps a Workspace socket open on every page (terminal/
// workspaceSocket.ts): every test's WebSockets are fakes, which a test of
// the terminal answers as the koko server and any other leaves unanswered.
beforeEach(() => {
  FakeWebSocket.sockets = [];
  vi.stubGlobal("WebSocket", FakeWebSocket);
});

afterEach(async () => {
  cleanup();
  // Imported here, not above: React Aria reads whether the document has
  // inert as it loads, so it must load after the stand-in above.
  const { toast } = await import("@heroui/react");
  withoutMotion(() => toast.clear());
  // React Aria's live announcer outlives a test, and what it announced, such
  // as a choice of several's "carol selected", names elements now gone.
  const { destroyAnnouncer } = await import("react-aria/private/live-announcer/LiveAnnouncer");
  destroyAnnouncer();
  const violations = takeCspViolations();
  window.history.replaceState(null, "", "/");
  localStorage.clear();
  resetMedia();
  document.documentElement.className = "";
  delete document.documentElement.dataset.theme;
  document.documentElement.lang = index.documentElement.lang;
  document.title = head.querySelector("title")?.textContent ?? "";
  expect(violations, "the CSP, default-src 'self', would refuse these").toEqual([]);
});
