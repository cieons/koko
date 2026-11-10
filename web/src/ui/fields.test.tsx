import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import { expect, test } from "vitest";

import { Providers } from "../shell/providers";
import { expectAccessible } from "../test/axe";
import { chooseOption, chooseRowAction, offeredOptions, searchAndChoose } from "../test/controls";
import { Choice, Field, type Option, SearchChoice, Segments } from "./fields";
import { RowMenu } from "./RowMenu";

// The fields of koko's forms, and the row menu.

const spaces: Option[] = ["ops", "Operations", "dev", "staging"].map((name) => ({ id: name, label: name }));

/** Picked shows a control's value as a page holds it. */
function Shown({ value }: { value: string | null }) {
  return <p>picked: {value ?? "none"}</p>;
}

test("a field in the code face keeps a machine value from being corrected, with its help under it", async () => {
  function Address() {
    const [value, setValue] = useState("");
    return (
      <>
        <Field label="Address" value={value} onChange={setValue} code description="An IP address or a DNS name." />
        <Shown value={value} />
      </>
    );
  }
  render(
    <Providers>
      <Address />
    </Providers>,
  );
  const input = screen.getByRole("textbox", { name: "Address" });

  expect(input.className).toContain("t-code");
  expect([input.getAttribute("spellcheck"), input.getAttribute("autocapitalize")]).toEqual(["false", "none"]);
  expect(input.getAttribute("aria-describedby")).toBe(screen.getByText("An IP address or a DNS name.").id);
  fireEvent.change(input, { target: { value: "192.0.2.10" } });
  expect(screen.getByText("picked: 192.0.2.10")).toBeDefined();
  await expectAccessible();
});

test("a Choice picks one item of a short fixed list", async () => {
  function Cycle() {
    const [value, setValue] = useState<string | null>("monthly");
    return (
      <>
        <Choice
          label="Billing cycle"
          value={value}
          onChange={setValue}
          options={[
            { id: "monthly", label: "Monthly" },
            { id: "yearly", label: "Yearly" },
          ]}
        />
        <Shown value={value} />
      </>
    );
  }
  render(
    <Providers>
      <Cycle />
    </Providers>,
  );

  await chooseOption("Monthly", "Yearly", ["Monthly", "Yearly"]);

  expect(screen.getByText("picked: yearly")).toBeDefined();
});

test("a SearchChoice narrows its list to the items holding what is typed, whatever its case, and takes one of them", async () => {
  function Space() {
    const [value, setValue] = useState<string | null>(null);
    return (
      <>
        <SearchChoice label="Space" value={value} onChange={setValue} options={spaces} />
        <Shown value={value} />
      </>
    );
  }
  render(
    <Providers>
      <Space />
    </Providers>,
  );

  await searchAndChoose("Space", "OP", "Operations", ["ops", "Operations"]);

  expect(screen.getByText("picked: Operations")).toBeDefined();
  expect(screen.getByRole<HTMLInputElement>("combobox", { name: "Space" }).value).toBe("Operations");
});

test("a SearchChoice takes no value its list does not hold", async () => {
  function Space() {
    const [value, setValue] = useState<string | null>(null);
    return (
      <>
        <SearchChoice label="Space" value={value} onChange={setValue} options={spaces} />
        <Shown value={value} />
      </>
    );
  }
  render(
    <Providers>
      <Space />
    </Providers>,
  );
  const input = screen.getByRole<HTMLInputElement>("combobox", { name: "Space" });

  act(() => input.focus());
  fireEvent.change(input, { target: { value: "nowhere" } });
  act(() => input.blur());

  await waitFor(() => expect(input.value).toBe(""));
  expect(screen.getByText("picked: none")).toBeDefined();
});

test("a SearchChoice with newValues takes a value its list does not hold, as an Attribute key does", async () => {
  function Key() {
    const [value, setValue] = useState<string | null>(null);
    return (
      <>
        <SearchChoice
          label="Attribute"
          value={value}
          onChange={setValue}
          options={["provider", "region", "price"].map((k) => ({ id: k, label: k }))}
          newValues
        />
        <Shown value={value} />
      </>
    );
  }
  render(
    <Providers>
      <Key />
    </Providers>,
  );
  const input = screen.getByRole<HTMLInputElement>("combobox", { name: "Attribute" });

  act(() => input.focus());
  fireEvent.change(input, { target: { value: "re" } });
  await waitFor(() => expect(offeredOptions()).toEqual(["region"]));
  fireEvent.change(input, { target: { value: "cores" } });
  act(() => input.blur());

  expect(screen.getByText("picked: cores")).toBeDefined();
  expect(input.value).toBe("cores");
  act(() => input.focus());
  fireEvent.change(input, { target: { value: "pro" } });
  fireEvent.click(within(await screen.findByRole("listbox")).getByRole("option", { name: "provider" }));
  await waitFor(() => expect(screen.getByText("picked: provider")).toBeDefined());
});

test("Segments choose one of a few, all in view, the chosen one pressed", async () => {
  function ValidFor() {
    const [value, setValue] = useState("1h");
    return (
      <>
        <Segments
          label="Valid for"
          value={value}
          onChange={setValue}
          options={[
            { id: "15m", label: "15 minutes" },
            { id: "1h", label: "1 hour" },
            { id: "1d", label: "1 day" },
            { id: "7d", label: "7 days" },
          ]}
        />
        <Shown value={value} />
      </>
    );
  }
  render(
    <Providers>
      <ValidFor />
    </Providers>,
  );
  const group = screen.getByRole("radiogroup", { name: "Valid for" });
  const pressed = () =>
    within(group)
      .getAllByRole("radio")
      .filter((b) => b.getAttribute("aria-checked") === "true")
      .map((b) => b.textContent);
  expect(pressed()).toEqual(["1 hour"]);
  await expectAccessible();

  fireEvent.click(within(group).getByRole("radio", { name: "7 days" }));

  expect(pressed()).toEqual(["7 days"]);
  expect(screen.getByText("picked: 7d")).toBeDefined();
});

test("a row menu offers its actions, the danger ones last, and runs the one chosen", async () => {
  const chosen: string[] = [];
  render(
    <Providers>
      <RowMenu
        label="More actions for web1"
        actions={[
          { id: "retire", label: "Retire", danger: true, onAction: () => chosen.push("retire") },
          { id: "rename", label: "Rename", onAction: () => chosen.push("rename") },
          { id: "move", label: "Move to another Space", isDisabled: true, onAction: () => chosen.push("move") },
        ]}
      />
    </Providers>,
  );

  fireEvent.click(screen.getByRole("button", { name: "More actions for web1" }));
  const menu = await screen.findByRole("menu", { name: "More actions for web1" });
  expect(within(menu).getAllByRole("menuitem").map((i) => i.textContent)).toEqual([
    "Rename",
    "Move to another Space",
    "Retire",
  ]);
  expect(within(menu).getByRole("menuitem", { name: "Move to another Space" }).getAttribute("aria-disabled")).toBe(
    "true",
  );
  fireEvent.keyDown(menu, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());

  await chooseRowAction("More actions for web1", "Retire");

  expect(chosen).toEqual(["retire"]);
});
