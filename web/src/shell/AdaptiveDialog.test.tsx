import { TextInput } from "@astryxdesign/core/TextInput";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { type ReactNode, useState } from "react";
import { expect, test, vi } from "vitest";

import { ApiError } from "../client";
import { expectAccessible } from "../test/axe";
import { setViewportWidth } from "../test/media";
import { AdaptiveDialog, FormDialog } from "./AdaptiveDialog";
import { Providers } from "./providers";

// The dialogs pages open for an action that never asks first: an Astryx
// Dialog, or a bottom sheet on a phone, that gives the focus back where it
// was, and the form in one that sends only when ready.

/** Opener opens what dialog renders from a button, and stops rendering it when it closes. */
function Opener({ dialog }: { dialog: (close: () => void) => ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <Providers>
      <button type="button" onClick={() => setOpen(true)}>
        Open
      </button>
      {open && dialog(() => setOpen(false))}
    </Providers>
  );
}

test("an adaptive dialog is a Dialog named by its title, described, that gives the focus back when it closes", async () => {
  const onClose = vi.fn();
  render(
    <Opener
      dialog={(close) => (
        <AdaptiveDialog
          title="Enroll web1"
          description="Run this on a device that has joined."
          onClose={() => {
            onClose();
            close();
          }}
          footer={<button type="button">Done</button>}
        >
          <p>koko host enroll web1</p>
        </AdaptiveDialog>
      )}
    />,
  );
  const open = screen.getByRole("button", { name: "Open" });
  open.focus();
  fireEvent.click(open);

  const dialog = await screen.findByRole("dialog", { name: "Enroll web1" });
  expect(within(dialog).getByText("Run this on a device that has joined.")).toBeDefined();
  expect(within(dialog).getByRole("button", { name: "Done" })).toBeDefined();
  await expectAccessible();

  fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
  await vi.waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(onClose).toHaveBeenCalledTimes(1);
  await vi.waitFor(() => expect(document.activeElement).toBe(open));
});

test("on a phone an adaptive dialog is a bottom sheet named by its title", async () => {
  setViewportWidth(390);
  render(
    <Opener
      dialog={(close) => (
        <AdaptiveDialog title="Columns" onClose={close}>
          <p>name</p>
        </AdaptiveDialog>
      )}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Open" }));

  const sheet = await screen.findByRole("dialog", { name: "Columns" });
  expect(within(sheet).getByRole("heading", { level: 2, name: "Columns" })).toBeDefined();
  await expectAccessible();
});

test("a form dialog sends only when ready, from its button or Enter, shows a refusal, and Cancel sends nothing", async () => {
  const onSubmit = vi.fn();
  function Form({ close, error }: { close: () => void; error?: unknown }) {
    const [name, setName] = useState("");
    return (
      <FormDialog title="Rename web1" submit="Rename" ready={name !== ""} error={error} onSubmit={onSubmit} onClose={close}>
        <TextInput label="New name" value={name} onChange={setName} />
      </FormDialog>
    );
  }
  const { rerender } = render(<Opener dialog={(close) => <Form close={close} />} />);
  fireEvent.click(screen.getByRole("button", { name: "Open" }));
  const dialog = within(await screen.findByRole("dialog", { name: "Rename web1" }));
  await expectAccessible();

  const act = dialog.getByRole("button", { name: "Rename" });
  expect(act.hasAttribute("disabled")).toBe(true);
  fireEvent.submit(dialog.getByRole("form", { name: "Rename web1" }));
  expect(onSubmit).not.toHaveBeenCalled();

  fireEvent.change(dialog.getByRole("textbox", { name: "New name" }), { target: { value: "web2" } });
  fireEvent.click(act);
  fireEvent.submit(dialog.getByRole("form", { name: "Rename web1" }));
  expect(onSubmit).toHaveBeenCalledTimes(2);

  rerender(<Opener dialog={(close) => <Form close={close} error={new ApiError("name_taken", "web2 is taken")} />} />);
  fireEvent.click(screen.getByRole("button", { name: "Open" }));
  const refused = within(await screen.findByRole("dialog", { name: "Rename web1" }));
  expect(refused.getAllByRole("alert").map((a) => a.textContent)).toContain("This name is already in use.");
  fireEvent.click(refused.getByRole("button", { name: "Cancel" }));
  await vi.waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(onSubmit).toHaveBeenCalledTimes(2);
});
