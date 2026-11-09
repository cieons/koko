import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Not one of core's exports: the module that holds the layer rules, by its path.
import { SLIDE_RULES_ID } from "../../node_modules/@astryxdesign/core/dist/Layer/layerSlideRules.js";
import { installMatchMedia, resetMedia } from "./media";

// jsdom has no modal dialogs: here showModal and close only open and close
// a dialog, and Escape is the dialog's cancel event.
HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
  this.open = true;
};
HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
  this.open = false;
};

// The document's head is index.html's: its title, and its link to Astryx's
// layer rules, without which Astryx injects them in a <style> element that
// the CSP refuses (static.test.ts checks both).
document.title = "koko";
document.head.append(
  Object.assign(document.createElement("link"), {
    rel: "stylesheet",
    id: SLIDE_RULES_ID,
    href: "/astryx-layer-rules.css",
  }),
);

// Astryx reads what jsdom lacks: media queries (the colour scheme, a
// phone's width), and the size and visibility of elements, which jsdom never
// lays out, so its observers here observe nothing.
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

// Each test starts from an empty document, at /, with nothing the browser
// remembers, and the system in light mode.
afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
  localStorage.clear();
  resetMedia();
});
