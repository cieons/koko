import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";

import { HostEnroll, type HostEnrollRequest } from "../../api/enroll";
import { type Host, HostAdd, HostList, HostShow } from "../../api/host";
import { type Secret, SecretList } from "../../api/secret";
import { expectAccessible } from "../../test/axe";
import { searchAndChoose } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, alerts, renderAt } from "../../test/pages";
import { encryptedKey, validFingerprint } from "./Enroll";
import { alice, host, records } from "./records.fixture";

// Enroll from the web UI, against the fake API: in Add Host, after its
// host.add, and on a Registered Host's page; with a password, a private key
// (with its passphrase when it is encrypted) or a Secret, and a host key's
// fingerprint checked as the CLI checks it; the credential cleared once sent
// and when the form closes, and offered only in a secure context.

// A test here renders the whole page and drives several overlays in turn;
// its limits are ceilings for a slow runner, not waits.
vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

afterEach(() => {
  vi.unstubAllGlobals();
});

const fingerprint = "SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s";

/**
 * opensshKey is a stand-in for a private key in OpenSSH's format whose
 * cipher is cipher: the page reads no more of a key than its cipher's name,
 * and the koko server, faked here, none of it.
 */
function opensshKey(cipher: string): string {
  const body = btoa(`openssh-key-v1\0\0\0\0${String.fromCharCode(cipher.length)}${cipher}\0\0\0\0stand-in`);
  return `-----BEGIN OPENSSH PRIVATE KEY-----\n${body}\n-----END OPENSSH PRIVATE KEY-----\n`;
}

const plainKey = opensshKey("none");
const encrypted = opensshKey("aes256-ctr");

const db2 = host("db2", { state: "registered", account: "root" });

function secret(space: string, name: string): Secret {
  return { id: `secret_${name}`, space, name, created_at: "2026-11-04T09:00:00Z", updated_at: "2026-11-05T09:00:00Z" };
}

/**
 * serve answers the Hosts page and each Host's page for hosts, host.add
 * once adding settles, and host.enroll once enroll has run, its Host then
 * Managed with the pin; secret.list answers ops/root-password and
 * dev/deploy-key.
 */
function serve(
  api: FakeApi,
  hosts: Host[],
  enroll: (req: HostEnrollRequest) => void | Promise<void> = () => {},
  adding: Promise<void> = Promise.resolve(),
): void {
  records(api, { hosts });
  const all = [...hosts];
  api.on(HostAdd, async (req) => {
    await adding;
    const added = req.hosts.map((h) => host(h.name, { space: h.space ?? "alice", account: h.account, state: "registered" }));
    all.push(...added);
    return { hosts: added };
  });
  api.on(HostShow, ({ host: named }) => {
    const shown = all.find((h) => h.id === named || h.name === named);
    if (shown === undefined) {
      throw refusal("unknown_host", 404);
    }
    return { host: shown, path: [], services: [] };
  });
  api.on(SecretList, ({ space }) => ({
    secrets: [secret("dev", "deploy-key"), secret("ops", "root-password")].filter((s) => space === undefined || s.space === space),
  }));
  api.on(HostEnroll, async (req) => {
    await enroll(req);
    const at = all.findIndex((h) => h.id === req.host);
    const managed = { ...(all[at] as Host), state: "managed" as const, host_key: fingerprint };
    all[at] = managed;
    return { host: managed };
  });
}

/** held is a promise and what settles it, for a call the test answers when it chooses. */
function held(): { promise: Promise<void>; release: () => void; refuse: (code: string) => void } {
  let release = () => {};
  let refuse = (_code: string) => {};
  const promise = new Promise<void>((resolve, reject) => {
    release = resolve;
    refuse = (code) => reject(refusal(code, 403));
  });
  return { promise, release, refuse };
}

/** openAddHost renders the Hosts page and opens Add Host. */
async function openAddHost(): Promise<HTMLElement> {
  renderAt("/hosts");
  await screen.findByRole("table", { name: "ops" });
  fireEvent.click(screen.getByRole("button", { name: "Add Host" }));
  return screen.findByRole("dialog", { name: "Add Host" });
}

/** fillAddHost names the new Host web3 and gives its address. */
function fillAddHost(panel: HTMLElement): void {
  fireEvent.change(within(panel).getByRole("textbox", { name: "Name" }), { target: { value: "web3" } });
  fireEvent.change(within(panel).getByRole("textbox", { name: "Address" }), { target: { value: "192.0.2.30" } });
}

/** openEnroll renders db2's page and opens Enroll. */
async function openEnroll(): Promise<HTMLElement> {
  renderAt("/hosts/db2");
  const reach = await screen.findByRole("region", { name: "Reaching it" });
  fireEvent.click(within(reach).getByRole("button", { name: "Enroll" }));
  return screen.findByRole("dialog", { name: "Enroll db2" });
}

/** way chooses the way to Enroll named name in holder. */
function way(holder: HTMLElement, name: string): void {
  fireEvent.click(within(within(holder).getByRole("radiogroup", { name: "Enroll with" })).getByRole("radio", { name }));
}

/** type types value into the field labelled label in holder: a password field has no textbox role. */
function type(holder: HTMLElement, label: string, value: string): void {
  fireEvent.change(within(holder).getByLabelText(label), { target: { value } });
}

/** valueOf is what the field labelled label in holder holds. */
function valueOf(holder: HTMLElement, label: string): string {
  return (within(holder).getByLabelText(label) as HTMLInputElement).value;
}

describe("Add Host", () => {
  test("records the Host, then Enrolls it by its id with the password and the fingerprint given, and moves to it", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const panel = await openAddHost();
    expect(within(panel).getByRole("radio", { name: "Not now" }).getAttribute("aria-checked")).toBe("true");
    fillAddHost(panel);
    way(panel, "Password");
    const password = within(panel).getByLabelText("Password of root");
    expect(password.getAttribute("type")).toBe("password");
    // Browsers offer to save no one-time code; the password managers read their own attributes.
    expect(password.getAttribute("autocomplete")).toBe("one-time-code");
    for (const attribute of ["data-1p-ignore", "data-lpignore", "data-bwignore", "data-form-type"]) {
      expect(password.hasAttribute(attribute), attribute).toBe(true);
    }
    type(panel, "Password of root", "pw 1");
    type(panel, "Host key fingerprint", fingerprint);
    await expectAccessible();
    fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));

    await waitFor(() => expect(window.location.pathname).toBe("/hosts/web3"));
    expect(api.calls.map((c) => c.path).filter((p) => p === HostAdd.path || p === HostEnroll.path)).toEqual([
      HostAdd.path,
      HostEnroll.path,
    ]);
    expect(api.requests(HostEnroll)).toEqual([{ host: "host_web3", password_base64: btoa("pw 1"), host_key: fingerprint }]);
    expect(await screen.findByText(`web3 is recorded and Managed, with its host key ${fingerprint} pinned.`)).toBeDefined();
  });

  test("records alone with Not now, as before", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const panel = await openAddHost();
    fillAddHost(panel);
    fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));

    await waitFor(() => expect(window.location.pathname).toBe("/hosts/web3"));
    expect(api.requests(HostEnroll)).toEqual([]);
    expect(await screen.findByText("web3 is recorded.")).toBeDefined();
  });

  test("Enrolls with a private key read from a file in the browser, which asks for no passphrase when it is not encrypted", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const panel = await openAddHost();
    fillAddHost(panel);
    way(panel, "Private key");
    const file = panel.querySelector<HTMLInputElement>("input[type=file]");
    expect(file).not.toBeNull();
    fireEvent.change(file as HTMLInputElement, { target: { files: [new File([plainKey], "id_ed25519")] } });
    await waitFor(() => expect(valueOf(panel, "Private key")).toBe(plainKey));
    expect(within(panel).queryByLabelText("Passphrase of the private key")).toBeNull();
    fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));

    await waitFor(() => expect(window.location.pathname).toBe("/hosts/web3"));
    expect(api.requests(HostEnroll)).toEqual([{ host: "host_web3", private_key_base64: btoa(plainKey) }]);
  });

  test("Enrolls with a Secret chosen among those of every Space, by its name alone", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const panel = await openAddHost();
    fillAddHost(panel);
    way(panel, "Secret");
    // secret.list is read once a Secret is the way chosen: its list opens once it has answered.
    const choice = within(panel).getByRole("combobox", { name: "Secret" });
    await waitFor(() => {
      fireEvent.click(choice);
      expect(screen.getByRole("listbox")).toBeDefined();
    });
    await searchAndChoose(choice, "", "ops/root-password", [
      "dev/deploy-key",
      "ops/root-password",
    ]);
    fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));

    await waitFor(() => expect(window.location.pathname).toBe("/hosts/web3"));
    expect(api.requests(SecretList)).toContainEqual({});
    expect(api.requests(HostEnroll)).toEqual([{ host: "host_web3", secret: { space: "ops", name: "root-password" } }]);
  });

  test("an Enroll that fails leaves the Host recorded, says why, and its page offers Enroll again", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2], () => {
      throw refusal("login_refused", 403);
    });
    const panel = await openAddHost();
    fillAddHost(panel);
    way(panel, "Password");
    type(panel, "Password of root", "not it");
    fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));

    await waitFor(() =>
      expect(alerts()).toContain("web3 is recorded, but not Enrolled: The Host refused the password or key for its Host Account."),
    );
    expect(within(panel).queryByLabelText("Password of root")).toBeNull();
    expect(api.requests(HostAdd)).toHaveLength(1);
    await expectAccessible();
    fireEvent.click(within(panel).getByRole("button", { name: "Open web3" }));

    await waitFor(() => expect(window.location.pathname).toBe("/hosts/web3"));
    const reach = await screen.findByRole("region", { name: "Reaching it" });
    expect(within(reach).getByRole("button", { name: "Enroll" })).toBeDefined();
    expect(api.requests(HostAdd)).toHaveLength(1);
  });

  test("Cancel clears what was typed: Add Host opens again with no credential", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    let panel = await openAddHost();
    way(panel, "Password");
    type(panel, "Password of root", "typed then cancelled");
    fireEvent.click(within(panel).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Add Host" }));
    panel = await screen.findByRole("dialog", { name: "Add Host" });
    expect(within(panel).getByRole("radio", { name: "Not now" }).getAttribute("aria-checked")).toBe("true");
    way(panel, "Password");
    expect(valueOf(panel, "Password of root")).toBe("");
    expect(api.requests(HostEnroll)).toEqual([]);
  });
  test("Cancel while host.add runs records the Host but sends no Enroll, and the list shows the Host", async () => {
    const api = fakeApi();
    actAs(api, alice);
    const adding = held();
    serve(api, [db2], () => {}, adding.promise);
    const panel = await openAddHost();
    fillAddHost(panel);
    way(panel, "Password");
    type(panel, "Password of root", "never sent");
    fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));
    await waitFor(() => expect(api.requests(HostAdd)).toHaveLength(1));
    fireEvent.click(within(panel).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const lists = api.requests(HostList).length;

    adding.release();
    expect(await screen.findByText("web3 is recorded.")).toBeDefined();
    await waitFor(() => expect(api.requests(HostList).length).toBeGreaterThan(lists));
    expect(api.requests(HostEnroll)).toEqual([]);
    expect(window.location.pathname).toBe("/hosts");
  });

  test("Close while host.enroll runs: its refusal comes as a toast, the list shows the Host, and the page stays", async () => {
    const api = fakeApi();
    actAs(api, alice);
    const enrolling = held();
    serve(api, [db2], () => enrolling.promise);
    const panel = await openAddHost();
    fillAddHost(panel);
    way(panel, "Password");
    type(panel, "Password of root", "pw");
    fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));
    await waitFor(() => expect(api.requests(HostEnroll)).toHaveLength(1));
    fireEvent.click(within(panel).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const lists = api.requests(HostList).length;

    enrolling.refuse("login_refused");
    expect(
      await screen.findByText("web3 is recorded, but not Enrolled: The Host refused the password or key for its Host Account."),
    ).toBeDefined();
    await waitFor(() => expect(api.requests(HostList).length).toBeGreaterThan(lists));
    expect(window.location.pathname).toBe("/hosts");
  });

  test("a key file longer than any key is refused before it is read", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const panel = await openAddHost();
    way(panel, "Private key");
    const large = new File([new Uint8Array((64 << 10) + 1)], "disk.img");
    const read = vi.spyOn(large, "text");
    fireEvent.change(panel.querySelector("input[type=file]") as HTMLInputElement, { target: { files: [large] } });
    expect(await within(panel).findByText("This is longer than 64 KiB, the most Enroll takes.")).toBeDefined();
    expect(read).not.toHaveBeenCalled();
    expect(valueOf(panel, "Private key")).toBe("");
  });

  test("a key file read after the way changed leaves the way and what was typed as they are", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const panel = await openAddHost();
    way(panel, "Private key");
    let give = (_text: string) => {};
    const file = new File([plainKey], "id_ed25519");
    file.text = () => new Promise<string>((resolve) => (give = resolve));
    fireEvent.change(panel.querySelector("input[type=file]") as HTMLInputElement, { target: { files: [file] } });
    way(panel, "Password");
    type(panel, "Password of root", "typed meanwhile");

    await act(async () => give(plainKey));
    expect(within(panel).getByRole("radio", { name: "Password" }).getAttribute("aria-checked")).toBe("true");
    expect(valueOf(panel, "Password of root")).toBe("typed meanwhile");
  });
});

describe("Enroll on a Registered Host's page", () => {
  test("Enrolls with a password by the Host's id, names the pin in a toast, and the page shows it", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const dialog = await openEnroll();
    expect(within(dialog).getByRole("radio", { name: "Password" }).getAttribute("aria-checked")).toBe("true");
    await expectAccessible();
    type(dialog, "Password of root", "pw");
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));

    expect(await screen.findByText(`db2 is Managed, with its host key ${fingerprint} pinned.`)).toBeDefined();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(api.requests(HostEnroll)).toEqual([{ host: "host_db2", password_base64: btoa("pw") }]);
    const reach = screen.getByRole("region", { name: "Reaching it" });
    await waitFor(() => expect(reach.textContent).toContain(fingerprint));
    expect(within(reach).queryByRole("button", { name: "Enroll" })).toBeNull();
  });

  test("an encrypted key asks for its passphrase, which goes with it; a refusal clears both, and only the same request again takes the same key", async () => {
    const api = fakeApi();
    actAs(api, alice);
    const refusals = ["wrong_passphrase", "host_unreachable"];
    serve(api, [db2], () => {
      const code = refusals.shift();
      if (code !== undefined) {
        throw refusal(code, 400);
      }
    });
    const dialog = await openEnroll();
    way(dialog, "Private key");
    type(dialog, "Private key", plainKey);
    expect(within(dialog).queryByLabelText("Passphrase of the private key")).toBeNull();
    type(dialog, "Private key", encrypted);
    const passphrase = within(dialog).getByLabelText("Passphrase of the private key");
    expect(passphrase.getAttribute("type")).toBe("password");
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() => expect(document.activeElement).toBe(passphrase));
    expect(api.requests(HostEnroll)).toEqual([]);

    type(dialog, "Passphrase of the private key", "not it");
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() => expect(alerts()).toContain("The passphrase does not open the private key."));
    expect(valueOf(dialog, "Private key")).toBe("");
    expect(within(dialog).queryByLabelText("Passphrase of the private key")).toBeNull();

    // The same request again, after a refusal that clears the credential too, takes the same key: the page kept only a digest of it.
    for (const outcome of ["koko could not connect to the Host.", undefined]) {
      type(dialog, "Private key", encrypted);
      type(dialog, "Passphrase of the private key", "opens it");
      fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
      if (outcome !== undefined) {
        await waitFor(() => expect(alerts()).toContain(outcome));
        expect(valueOf(dialog, "Private key")).toBe("");
      }
    }
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const opens = { host: "host_db2", private_key_base64: btoa(encrypted), passphrase_base64: btoa("opens it") };
    expect(api.requests(HostEnroll)).toEqual([
      { host: "host_db2", private_key_base64: btoa(encrypted), passphrase_base64: btoa("not it") },
      opens,
      opens,
    ]);
    const [first, second, third] = api.calls.filter((c) => c.path === HostEnroll.path).map((c) => c.headers["idempotency-key"]);
    expect(first).not.toBe(second);
    expect(third).toBe(second);
  });

  test("the fingerprint is checked as koko host enroll --host-key checks it, before any call", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const dialog = await openEnroll();
    type(dialog, "Password of root", "pw");
    for (const wrong of ["SHA256:short", `MD5:${fingerprint.slice(7)}`, `${fingerprint.slice(0, -1)}t`]) {
      type(dialog, "Host key fingerprint", wrong);
      fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
      expect(
        await within(dialog).findByText("A fingerprint is SHA256: and 43 characters of base64, as ssh-keygen -lf prints it."),
      ).toBeDefined();
    }
    expect(api.requests(HostEnroll)).toEqual([]);

    type(dialog, "Host key fingerprint", ` ${fingerprint} `);
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() => expect(api.requests(HostEnroll)).toEqual([{ host: "host_db2", password_base64: btoa("pw"), host_key: fingerprint }]));
  });

  test("Cancel clears the credential: Enroll opens again empty", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    let dialog = await openEnroll();
    type(dialog, "Password of root", "typed then cancelled");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Enroll" }));
    dialog = await screen.findByRole("dialog", { name: "Enroll db2" });
    expect(valueOf(dialog, "Password of root")).toBe("");
    expect(api.requests(HostEnroll)).toEqual([]);
  });
  test("invalid_value says what Enroll takes, not what the Host form does", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2], () => {
      throw refusal("invalid_value", 400);
    });
    const dialog = await openEnroll();
    way(dialog, "Private key");
    type(dialog, "Private key", "-----BEGIN ENCRYPTED PRIVATE KEY-----\nAAAA\n-----END ENCRYPTED PRIVATE KEY-----\n");
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() =>
      expect(alerts()).toContain(
        "Enroll does not take this private key or this Secret's value: a key must be in OpenSSH, PKCS #1, PKCS #8 or SEC 1 form, one held in a Secret must be stored unencrypted, and a password must hold more than a line ending.",
      ),
    );
  });

  test("passphrase_needed for a key the page read as plain asks for the passphrase, sent with the key next time", async () => {
    const api = fakeApi();
    actAs(api, alice);
    const refusals = ["passphrase_needed"];
    serve(api, [db2], () => {
      const code = refusals.shift();
      if (code !== undefined) {
        throw refusal(code, 400);
      }
    });
    const dialog = await openEnroll();
    way(dialog, "Private key");
    type(dialog, "Private key", plainKey);
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() => expect(alerts()).toContain("The private key is encrypted; give its passphrase."));

    type(dialog, "Private key", plainKey);
    type(dialog, "Passphrase of the private key", "opens it");
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(api.requests(HostEnroll)).toEqual([
      { host: "host_db2", private_key_base64: btoa(plainKey) },
      { host: "host_db2", private_key_base64: btoa(plainKey), passphrase_base64: btoa("opens it") },
    ]);
  });

  test("an Enroll whose answer dropped, given again, takes the same key, so the koko server answers it once", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const dialog = await openEnroll();
    type(dialog, "Password of root", "pw");
    api.drop(2);
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() => expect(alerts().length).toBeGreaterThan(0));
    expect(valueOf(dialog, "Password of root")).toBe("");

    type(dialog, "Password of root", "pw");
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const keys = api.calls.filter((c) => c.path === HostEnroll.path).map((c) => c.headers["idempotency-key"]);
    expect(keys).toHaveLength(3);
    expect(new Set(keys).size).toBe(1);
  });

  test("already_managed loads the page again, which then shows the Host Managed", async () => {
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const dialog = await openEnroll();
    // Another Enroll, from the CLI, made db2 Managed meanwhile.
    api.on(HostShow, () => ({ host: { ...db2, state: "managed", host_key: fingerprint }, path: [], services: [] }));
    api.on(HostEnroll, () => {
      throw refusal("already_managed", 409);
    });
    type(dialog, "Password of root", "pw");
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() => expect(alerts()).toContain("The Host is Managed already; Enroll it again only to pin a new host key."));
    await waitFor(() => expect(screen.getByRole("region", { name: "Reaching it" }).textContent).toContain(fingerprint));
  });
});

describe("outside a secure context", () => {
  test("Enroll offers a Secret alone, says why, and sends its name", async () => {
    vi.stubGlobal("isSecureContext", false);
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const dialog = await openEnroll();
    expect(within(dialog).queryByRole("radiogroup")).toBeNull();
    expect(dialog.querySelector("input[type=password], textarea, input[type=file]")).toBeNull();
    expect(dialog.textContent).toContain("This connection is not secure");
    await searchAndChoose(within(dialog).getByRole("combobox", { name: "Secret" }), "dep", "dev/deploy-key");
    fireEvent.click(within(dialog).getByRole("button", { name: "Enroll" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(api.requests(HostEnroll)).toEqual([{ host: "host_db2", secret: { space: "dev", name: "deploy-key" } }]);
  });

  test("Add Host offers Not now and a Secret alone", async () => {
    vi.stubGlobal("isSecureContext", false);
    const api = fakeApi();
    actAs(api, alice);
    serve(api, [db2]);
    const panel = await openAddHost();
    const ways = within(within(panel).getByRole("radiogroup", { name: "Enroll with" })).getAllByRole("radio");
    expect(ways.map((w) => w.textContent)).toEqual(["Not now", "Secret"]);
    expect(panel.querySelector("input[type=password], input[type=file]")).toBeNull();
  });
});

test("validFingerprint takes SHA256: and 43 characters of base64 whose last two bits are zero, as Go's strict decoding does", () => {
  expect(validFingerprint(fingerprint)).toBe(true);
  expect(validFingerprint(`${fingerprint.slice(0, -1)}t`)).toBe(false);
  expect(validFingerprint(`${fingerprint}=`)).toBe(false);
  expect(validFingerprint(fingerprint.slice(7))).toBe(false);
});

test("encryptedKey reads the cipher of a key in OpenSSH's format and the Proc-Type of a PEM block", () => {
  expect(encryptedKey(plainKey)).toBe(false);
  expect(encryptedKey(encrypted)).toBe(true);
  expect(encryptedKey(`  \n${encrypted}`)).toBe(true);
  expect(encryptedKey("-----BEGIN RSA PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\nDEK-Info: AES-128-CBC,00\n\nAAAA\n-----END RSA PRIVATE KEY-----\n")).toBe(true);
  expect(encryptedKey("-----BEGIN RSA PRIVATE KEY-----\nAAAA\n-----END RSA PRIVATE KEY-----\n")).toBe(false);
  expect(encryptedKey("a password, not a key")).toBe(false);
  expect(encryptedKey("-----BEGIN OPENSSH PRIVATE KEY-----\n!!!\n-----END OPENSSH PRIVATE KEY-----")).toBe(false);
  // Windows' line endings, as a key copied there holds them.
  expect(encryptedKey(encrypted.replaceAll("\n", "\r\n"))).toBe(true);
  expect(encryptedKey(plainKey.replaceAll("\n", "\r\n"))).toBe(false);
  // SEC 1, and a Proc-Type indented, as Go's pem.Decode still reads it.
  expect(encryptedKey("-----BEGIN EC PRIVATE KEY-----\r\nProc-Type: 4,ENCRYPTED\r\nDEK-Info: AES-256-CBC,00\r\n\r\nAAAA\r\n-----END EC PRIVATE KEY-----\r\n")).toBe(true);
  expect(encryptedKey("-----BEGIN RSA PRIVATE KEY-----\n  Proc-Type: 4,ENCRYPTED\nDEK-Info: AES-128-CBC,00\n\nAAAA\n-----END RSA PRIVATE KEY-----\n")).toBe(true);
  // PKCS #8's encrypted form, which x/crypto does not read: no passphrase is asked, and the koko server says why.
  expect(encryptedKey("-----BEGIN ENCRYPTED PRIVATE KEY-----\nAAAA\n-----END ENCRYPTED PRIVATE KEY-----\n")).toBe(false);
});
