import { Button, Modal } from "@heroui/react";
import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

import { Shell } from "../shell/Shell";
import { expectAccessible } from "./axe";
import { fakeApi } from "./fakeApi";
import { actAs } from "./pages";

// The accessibility check page tests call: it passes on what meets WCAG 2.2
// AA and names each violation of what does not.

test("the accessibility check passes on the frame with an open HeroUI dialog", async () => {
  const api = fakeApi();
  actAs(api, { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" });
  function DialogPage() {
    return (
      <Modal.Backdrop isOpen>
        <Modal.Container>
          <Modal.Dialog>
            <Modal.CloseTrigger aria-label="Close" />
            <Modal.Header>
              <Modal.Heading>Details</Modal.Heading>
            </Modal.Header>
            <Modal.Body>
              <p>What the dialog says.</p>
            </Modal.Body>
            <Modal.Footer>
              <Button>Done</Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
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
