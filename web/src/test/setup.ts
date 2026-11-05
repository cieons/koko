import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// jsdom has no modal dialogs: here showModal and close only open and close
// a dialog, and Escape is the dialog's cancel event.
HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
  this.open = true;
};
HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
  this.open = false;
};

// Each test starts from an empty document, at /, with nothing the browser
// remembers.
afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
  localStorage.clear();
});
