import { Button } from "@heroui/react";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { type ReactNode, useState } from "react";
import { expect, test, vi } from "vitest";

import { ApiError } from "../client";
import { Providers } from "../shell/providers";
import { expectAccessible } from "../test/axe";
import { clickOutside } from "../test/controls";
import { setViewportWidth } from "../test/media";
import { alerts } from "../test/pages";
import { ConfirmDialog } from "./ConfirmDialog";
import { Field } from "./fields";
import { FormDialog } from "./FormDialog";
import { SidePanel } from "./SidePanel";

// The dialogs pages open: a short form, a side panel and a confirmation,
// each open while the page renders it.

/**
 * renderOpener renders a button that opens the dialog dialog gives, which
 * it closes through close, and is the button, focused and pressed, as a
 * click or a key leaves one in a browser.
 */
async function renderOpener(dialog: (close: () => void) => ReactNode): Promise<HTMLElement> {
  function Opener() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <Button onPress={() => setOpen(true)}>open</Button>
        {open && dialog(() => setOpen(false))}
      </>
    );
  }
  render(
    <Providers>
      <Opener />
    </Providers>,
  );
  const opener = screen.getByRole("button", { name: "open" });
  opener.focus();
  fireEvent.click(opener);
  return opener;
}

/** A rename's short form, as a page would write it. */
function RenameDialog(props: {
  onSubmit: (name: string) => void;
  onClose: () => void;
  pending?: boolean;
  error?: unknown;
}) {
  const [name, setName] = useState("");
  return (
    <FormDialog
      title="Rename web1"
      lead="Commands that use the old name stop working."
      submit="Rename"
      pending={props.pending ?? false}
      changed={name !== ""}
      error={props.error}
      onSubmit={() => props.onSubmit(name)}
      onClose={props.onClose}
    >
      <Field
        label="New name"
        value={name}
        onChange={setName}
        code
        validate={(v) => (v === "" ? "Give the new name." : undefined)}
      />
      <Field label="Note" value="" onChange={() => {}} />
    </FormDialog>
  );
}

// A short form.

test("a short form validates when it is submitted: the error under its field, the focus on it, nothing sent", async () => {
  const onSubmit = vi.fn();
  await renderOpener((close) => <RenameDialog onSubmit={onSubmit} onClose={close} />);
  const dialog = await screen.findByRole("dialog", { name: "Rename web1" });
  const form = within(dialog).getByRole("form", { name: "Rename web1" });
  expect(form.hasAttribute("novalidate")).toBe(true);
  await expectAccessible();

  const submit = within(dialog).getByRole("button", { name: "Rename" });
  expect(submit.hasAttribute("disabled")).toBe(false);
  fireEvent.click(submit);

  const field = within(dialog).getByRole("textbox", { name: "New name" });
  await waitFor(() => expect(document.activeElement).toBe(field));
  expect(field.getAttribute("aria-invalid")).toBe("true");
  expect(within(dialog).getByText("Give the new name.")).toBeDefined();
  expect(onSubmit).not.toHaveBeenCalled();
  await expectAccessible();

  fireEvent.change(field, { target: { value: "web2" } });
  fireEvent.submit(form); // Enter in a field

  expect(onSubmit).toHaveBeenCalledWith("web2");
});

test("a short form's refusal shows in place, the form kept as typed, and a pending call takes no second press", async () => {
  const onSubmit = vi.fn();
  let refuse = () => {};
  function Refused() {
    const [error, setError] = useState<unknown>();
    const [pending, setPending] = useState(false);
    return (
      <RenameDialog
        pending={pending}
        error={error}
        onClose={() => {}}
        onSubmit={(name) => {
          onSubmit(name);
          setPending(true);
          refuse = () => {
            setPending(false);
            setError(new ApiError("name_taken", "taken"));
          };
        }}
      />
    );
  }
  await renderOpener(() => <Refused />);
  const dialog = await screen.findByRole("dialog", { name: "Rename web1" });
  const field = within(dialog).getByRole("textbox", { name: "New name" });
  fireEvent.change(field, { target: { value: "db1" } });
  const submit = within(dialog).getByRole("button", { name: "Rename" });

  fireEvent.click(submit);
  await waitFor(() => expect(submit.getAttribute("data-pending")).toBe("true"));
  fireEvent.click(submit); // while the call runs
  expect(onSubmit).toHaveBeenCalledOnce();
  act(() => refuse());

  await waitFor(() => expect(alerts()).toEqual(["This name is already in use."]));
  expect(onSubmit).toHaveBeenCalledOnce();
  expect((field as HTMLInputElement).value).toBe("db1");
  await expectAccessible();
});

test("Cancel, Escape and the close button close a short form without sending it, and the focus goes back", async () => {
  for (const close of ["Cancel", "Escape", "Close"]) {
    const onSubmit = vi.fn();
    const opener = await renderOpener((onClose) => <RenameDialog onSubmit={onSubmit} onClose={onClose} />);
    const dialog = await screen.findByRole("dialog", { name: "Rename web1" });

    if (close === "Escape") {
      fireEvent.keyDown(within(dialog).getByRole("textbox", { name: "New name" }), { key: "Escape" });
    } else {
      fireEvent.click(within(dialog).getByRole("button", { name: close }));
    }

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(opener));
    expect(onSubmit).not.toHaveBeenCalled();
    document.body.replaceChildren();
  }
});

test("a click beside a short form closes it while nothing in it has changed, the focus going back, but not once it has or while its call runs", async () => {
  const opener = await renderOpener((close) => <RenameDialog onSubmit={() => {}} onClose={close} />);
  await screen.findByRole("dialog", { name: "Rename web1" });
  await clickOutside();
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(opener));

  fireEvent.click(opener);
  const dialog = await screen.findByRole("dialog", { name: "Rename web1" });
  const field = within(dialog).getByRole("textbox", { name: "New name" });
  fireEvent.change(field, { target: { value: "db1" } });
  await clickOutside();
  expect(screen.getByRole("dialog", { name: "Rename web1" })).toBe(dialog);
  expect((field as HTMLInputElement).value).toBe("db1");
  cleanup();

  const onClose = vi.fn();
  await renderOpener(() => <RenameDialog onSubmit={() => {}} onClose={onClose} pending />);
  await screen.findByRole("dialog", { name: "Rename web1" });
  await clickOutside();
  expect(onClose).not.toHaveBeenCalled();
});

// A side panel.

test("a side panel comes from the right on a desktop and from the bottom on a phone, named by its title", async () => {
  for (const [width, placement] of [
    [1280, "right"],
    [390, "bottom"],
  ] as const) {
    setViewportWidth(width);
    const opener = await renderOpener((close) => (
      <SidePanel title="db1" subtitle="On web1" onClose={close} footer={<Button>Save</Button>}>
        <p>The Service's fields.</p>
      </SidePanel>
    ));

    const panel = await screen.findByRole("dialog", { name: "db1" });
    expect(panel.closest(`[data-placement="${placement}"]`)).not.toBeNull();
    expect(panel.textContent).toContain("On web1");
    await expectAccessible();
    fireEvent.keyDown(panel, { key: "Escape" });

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(opener));
    document.body.replaceChildren();
  }
});

test("a side panel with onSubmit is a form that validates on submit, sent by the submit button in its footer", async () => {
  const onSubmit = vi.fn();
  function Panel({ onClose }: { onClose: () => void }) {
    const [name, setName] = useState("");
    return (
      <SidePanel
        title="Add Service"
        onClose={onClose}
        onSubmit={() => onSubmit(name)}
        changed={name !== ""}
        pending={false}
        footer={<Button type="submit">Add</Button>}
      >
        <Field label="Name" value={name} onChange={setName} validate={(v) => (v === "" ? "Give a name." : undefined)} />
      </SidePanel>
    );
  }
  await renderOpener((close) => <Panel onClose={close} />);
  const panel = await screen.findByRole("dialog", { name: "Add Service" });

  fireEvent.click(within(panel).getByRole("button", { name: "Add" }));
  expect(await within(panel).findByText("Give a name.")).toBeDefined();
  fireEvent.change(within(panel).getByRole("textbox", { name: "Name" }), { target: { value: "db" } });
  fireEvent.click(within(panel).getByRole("button", { name: "Add" }));

  expect(onSubmit).toHaveBeenCalledWith("db");
});

test("a click beside a side panel's form does not close it while its call runs", async () => {
  const onClose = vi.fn();
  render(
    <Providers>
      <SidePanel title="Add Service" onClose={onClose} onSubmit={() => {}} changed={false} pending>
        <p>The Service's fields.</p>
      </SidePanel>
    </Providers>,
  );
  await screen.findByRole("dialog", { name: "Add Service" });
  await clickOutside();
  expect(onClose).not.toHaveBeenCalled();
});

// A confirmation.

function Confirm(props: {
  onConfirm: () => void;
  onCancel: () => void;
  typedName?: string;
  pending?: boolean;
  error?: unknown;
}) {
  return (
    <ConfirmDialog
      title="Revoke laptop"
      consequence="laptop can never connect again."
      confirm="Revoke"
      {...props}
    />
  );
}

test("an irreversible action asks first, stating the consequence, with the focus on Cancel", async () => {
  const onConfirm = vi.fn();
  const opener = await renderOpener((close) => <Confirm onConfirm={onConfirm} onCancel={close} />);

  const dialog = await screen.findByRole("alertdialog", { name: "Revoke laptop" });
  expect(dialog.getAttribute("aria-describedby")).toBe(within(dialog).getByText("laptop can never connect again.").id);
  await waitFor(() => expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "Cancel" })));
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(opener));
  expect(onConfirm).not.toHaveBeenCalled();
});

test("a confirmation acts on its button, closes on Escape without acting, but not while its call runs", async () => {
  const onConfirm = vi.fn();
  const onCancel = vi.fn();
  const { rerender } = render(
    <Providers>
      <Confirm onConfirm={onConfirm} onCancel={onCancel} />
    </Providers>,
  );
  const dialog = await screen.findByRole("alertdialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));
  expect(onConfirm).toHaveBeenCalledOnce();

  fireEvent.keyDown(dialog, { key: "Escape" });
  expect(onCancel).toHaveBeenCalledOnce();

  rerender(
    <Providers>
      <Confirm onConfirm={onConfirm} onCancel={onCancel} pending error={new ApiError("last_device", "last")} />
    </Providers>,
  );
  fireEvent.keyDown(screen.getByRole("alertdialog"), { key: "Escape" });
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: /Revoke/ }));
  expect(onCancel).toHaveBeenCalledOnce();
  expect(onConfirm).toHaveBeenCalledOnce();
  expect(alerts()).toHaveLength(1);
});

test("the typed-name variant confirms only once the name is typed exactly, its field taking the focus", async () => {
  const onConfirm = vi.fn();
  const opener = await renderOpener((close) => <Confirm onConfirm={onConfirm} onCancel={close} typedName="laptop" />);
  const dialog = await screen.findByRole("alertdialog", { name: "Revoke laptop" });
  const confirm = within(dialog).getByRole("button", { name: "Revoke" });
  const input = within(dialog).getByRole("textbox", { name: "Type laptop to confirm" });

  await waitFor(() => expect(document.activeElement).toBe(input));
  await expectAccessible();
  for (const typed of ["", "lapto", "Laptop"]) {
    fireEvent.change(input, { target: { value: typed } });
    expect(confirm.hasAttribute("disabled")).toBe(true);
    fireEvent.click(confirm);
  }
  expect(onConfirm).not.toHaveBeenCalled();
  fireEvent.change(input, { target: { value: "laptop" } });
  expect(confirm.hasAttribute("disabled")).toBe(false);
  fireEvent.click(confirm);
  expect(onConfirm).toHaveBeenCalledOnce();

  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(document.activeElement).toBe(opener));
});
