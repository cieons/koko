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
 * searchAndChoose types typed in the searchable choice named choice, or the
 * one given, as a person narrows its list, checks the list open with axe,
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
  fireEvent.change(input, { target: { value: typed } });
  const listbox = await screen.findByRole("listbox");
  await expectAccessible(listbox);
  if (offered) {
    await waitFor(() => expect(offeredOptions()).toEqual(offered));
  }
  fireEvent.click(within(listbox).getByRole("option", { name: option }));
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
}
