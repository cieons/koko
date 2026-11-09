import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect } from "vitest";

import { expectAccessible } from "./axe";

// Helpers for the tests of pages that use Astryx's own controls, whose
// lists open in a popover: a row's overflow menu and a Selector.

/**
 * chooseRowAction opens the overflow menu named menu, such as "Actions for
 * ops/db", checks the page with the menu open with axe, and chooses its
 * item named action.
 */
export async function chooseRowAction(menu: string, action: string): Promise<void> {
  fireEvent.click(screen.getByRole("button", { name: menu }));
  const item = await screen.findByRole("menuitem", { name: action });
  await expectAccessible();
  fireEvent.click(item);
}

/**
 * chooseOption opens the Selector combobox, checks the page with its list
 * open with axe, checks that it offers the options named offered, in order,
 * when given, and chooses the one named option.
 */
export async function chooseOption(combobox: HTMLElement, option: string, offered?: string[]): Promise<void> {
  fireEvent.click(combobox);
  const listbox = await screen.findByRole("listbox");
  await expectAccessible();
  if (offered) {
    await waitFor(() =>
      expect(
        within(listbox)
          .getAllByRole("option")
          .map((o) => o.textContent),
      ).toEqual(offered),
    );
  }
  fireEvent.click(within(listbox).getByRole("option", { name: option }));
  await waitFor(() => expect(combobox.textContent).toContain(option));
}
