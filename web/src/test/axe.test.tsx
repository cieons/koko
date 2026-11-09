import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

import { AttrList } from "../api/attr";
import { HostList } from "../api/host";
import { Shell } from "../shell/Shell";
import { expectAccessible } from "./axe";
import { fakeApi } from "./fakeApi";
import { actAs, renderAt } from "./pages";

// The accessibility check page tests call: it passes on what meets WCAG 2.2
// AA and names each violation of what does not.

test("the accessibility check passes on the web UI's first page and on an open Astryx dialog", async () => {
  const api = fakeApi();
  actAs(api, { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" });
  api.on(AttrList, () => ({ attributes: [] }));
  api.on(HostList, () => ({ hosts: [] }));
  const { unmount } = renderAt("/hosts");
  await screen.findByRole("heading", { name: "No Host matches this view." });
  await expectAccessible();
  unmount();

  function DialogPage() {
    return (
      <Dialog isOpen onOpenChange={() => {}}>
        <DialogHeader title="Details" onOpenChange={() => {}} />
      </Dialog>
    );
  }
  window.history.replaceState(null, "", "/dialog");
  render(<Shell routes={[{ path: "/dialog", page: DialogPage }]} />);
  await screen.findByRole("dialog", { name: "Details" });
  await expectAccessible();
});

test("the accessibility check fails on a control without a label, naming the rule and the element", async () => {
  render(
    <main>
      <input type="text" />
    </main>,
  );
  await expect(expectAccessible()).rejects.toThrow(/label[\s\S]*<input type="text">/);
});
