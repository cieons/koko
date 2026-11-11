import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect } from "vitest";

import { expectAccessible } from "./axe";

// Helpers for the tests of pages that use React Aria's controls, whose
// lists open in a popover: a row menu, a select and a searchable choice.

/**
 * chooseRowAction opens the menu behind the button named menu, such as
 * "More actions for web1", checks the page with the menu open with axe, and
 * chooses its item named action.
 */
export async function chooseRowAction(menu: string, action: string): Promise<void> {
  fireEvent.click(screen.getByRole("button", { name: menu }));
  const item = await screen.findByRole("menuitem", { name: action });
  await expectAccessible();
  fireEvent.click(item);
  await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
}

/**
 * clickOutside clicks beside the overlay open now, as a browser does: the
 * press takes the focus off the control that holds it, a frame passes, in
 * which a modal overlay's focus scope gives that control the focus back,
 * and the overlay hears the click as the press ends.
 */
export async function clickOutside(): Promise<void> {
  const at = document.body;
  const press = { pointerId: 1, pointerType: "mouse", button: 0 };
  fireEvent.pointerDown(at, press);
  fireEvent.mouseDown(at, press);
  act(() => {
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
    }
  });
  await nextFrame();
  fireEvent.pointerUp(at, press);
  fireEvent.mouseUp(at, press);
  fireEvent.click(at, press);
}

/** nextFrame lets a frame pass, and what React Aria does in it, such as a focus scope taking the focus back. */
export async function nextFrame(): Promise<void> {
  await act(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())));
}

/** offeredOptions are the names of the options of the list open now. */
export function offeredOptions(): string[] {
  return within(screen.getByRole("listbox"))
    .getAllByRole("option")
    .map((o) => o.textContent ?? "");
}

/**
 * chooseOption opens the select named select, checks the page with its list
 * open with axe, checks that it offers the options named offered, in order,
 * when given, and chooses the one named option.
 */
export async function chooseOption(select: string, option: string, offered?: string[]): Promise<void> {
  fireEvent.click(screen.getByRole("button", { name: new RegExp(`^${select}`) }));
  const listbox = await screen.findByRole("listbox");
  await expectAccessible();
  if (offered) {
    expect(offeredOptions()).toEqual(offered);
  }
  fireEvent.click(within(listbox).getByRole("option", { name: option }));
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
}

/**
 * searchAndChoose presses the searchable choice named choice, or the one
 * given, which opens its whole list, and types typed in it, unless it is
 * empty, as a person narrows the list; then it checks the list open with axe,
 * checks the options it then offers, when offered is given, and chooses the
 * one named option.
 *
 * axe checks the list alone: while it is open, React Aria hides the rest of
 * the page from assistive technology, its controls with it, so axe would
 * fault each of them as focusable yet hidden; but the focus stays in the
 * field, and Tab closes the list, and with it the hiding, before it moves.
 */
export async function searchAndChoose(
  choice: string | HTMLElement,
  typed: string,
  option: string,
  offered?: string[],
): Promise<void> {
  const input = typeof choice === "string" ? screen.getByRole("combobox", { name: choice }) : choice;
  act(() => input.focus());
  fireEvent.click(input);
  if (typed !== "") {
    fireEvent.change(input, { target: { value: typed } });
  }
  const listbox = await screen.findByRole("listbox");
  await expectAccessible(listbox);
  if (offered) {
    await waitFor(() => expect(offeredOptions()).toEqual(offered));
  }
  fireEvent.click(within(listbox).getByRole("option", { name: option }));
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
}

/**
 * searchAndChooseSeveral opens the searchable choice of several named
 * choice, within holder when given, types typed in the search field over its
 * list, unless it is empty, checks the list open with axe, checks the
 * options it offers, when offered is given, picks each of options, then
 * closes the list with Escape.
 */
export async function searchAndChooseSeveral(
  choice: string,
  typed: string,
  options: readonly string[],
  { holder = document.body, offered }: { holder?: HTMLElement; offered?: string[] } = {},
): Promise<void> {
  // The field, which a press anywhere on opens, holds the button named by
  // what is picked, then the label.
  const button = within(holder).getByRole("button", { name: new RegExp(`${choice}$`) });
  const field = button.closest<HTMLElement>("[data-slot=autocomplete-trigger]");
  expect(field).not.toBeNull();
  fireEvent.click(field as HTMLElement);
  const listbox = await screen.findByRole("listbox");
  if (typed !== "") {
    const popover = listbox.closest<HTMLElement>("[data-slot=autocomplete-popover]");
    expect(popover).not.toBeNull();
    fireEvent.change(within(popover as HTMLElement).getByRole("searchbox"), { target: { value: typed } });
  }
  await expectAccessible(listbox);
  if (offered) {
    await waitFor(() => expect(offeredOptions()).toEqual(offered));
  }
  for (const option of options) {
    fireEvent.click(within(listbox).getByRole("option", { name: option }));
  }
  fireEvent.keyDown(listbox, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
}

/**
 * expectChosen waits for the searchable choice, by its name or itself, to
 * show text: it first shows with its field empty, and React Aria fills in
 * the item chosen a render later.
 */
export async function expectChosen(choice: string | HTMLElement, text: string): Promise<void> {
  await waitFor(() => {
    const input = typeof choice === "string" ? screen.getByRole<HTMLInputElement>("combobox", { name: choice }) : choice;
    expect((input as HTMLInputElement).value).toBe(text);
  });
}
