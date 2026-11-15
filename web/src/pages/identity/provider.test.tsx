import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import {
  type IdentityProvider,
  ServerOIDCRemove,
  ServerOIDCSet,
  ServerOIDCShow,
  type ServerOIDCShowResponse,
} from "../../api/identity";
import type { User } from "../../api/user";
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, alerts, preferLanguages, renderAt } from "../../test/pages";
import { administered, alice, bob, eve, serve, world } from "../space/access.fixture";

// The Identity provider panel over Access, against a fake koko server: the
// Owner's form, an Administrator's read-only view, and the panel without a
// Web URL.

const redirect = "https://koko.example.com/auth/oidc/callback";

const authentik: IdentityProvider = {
  issuer: "https://idp.example.com/application/o/koko/",
  client_id: "koko",
  name: "Authentik",
  client_secret_set: true,
  linked_identities: 2,
  set_at: "2026-11-15T09:00:00Z",
};

/**
 * at renders /access/provider as caller against a fake koko server whose
 * server.oidc.show answers shown, and server.oidc.set and remove as the
 * koko server does, and waits for the panel.
 */
async function at(
  shown: ServerOIDCShowResponse,
  caller: User = alice,
  path = "/access/provider",
): Promise<{ api: FakeApi; panel: HTMLElement }> {
  const api = fakeApi();
  const w = administered(world());
  actAs(api, caller, w.devices.find((d) => d.user === caller.name && d.device === "laptop"));
  serve(api, w, caller);
  let provider = shown.provider;
  api.on(ServerOIDCShow, () => ({ ...shown, provider }));
  api.on(ServerOIDCSet, (req) => {
    const changed = provider !== null && req.issuer !== undefined && req.issuer !== provider.issuer;
    provider = {
      issuer: req.issuer ?? provider?.issuer ?? "",
      client_id: req.client_id ?? provider?.client_id ?? "",
      name: req.name ?? provider?.name ?? "",
      client_secret_set: true,
      linked_identities: changed ? 0 : (provider?.linked_identities ?? 0),
      set_at: "2026-11-15T10:00:00Z",
    };
    return { provider, redirect_uri: redirect, identities_unlinked: changed ? 2 : 0 };
  });
  api.on(ServerOIDCRemove, () => {
    provider = null;
    return { identities_unlinked: 2 };
  });
  renderAt(path);
  const panel = await screen.findByRole("dialog", { name: /^(Identity provider|身份提供方)/ });
  await within(panel).findAllByText(/Redirect URI|重定向 URI|Web URL|Web 地址/);
  return { api, panel };
}

/** type types value into the field labelled label in panel. */
function type(panel: HTMLElement, label: string, value: string): void {
  fireEvent.change(within(panel).getByLabelText(label), { target: { value } });
}

test("Access offers Identity provider beside Invite to the Owner and an Administrator, opening its panel", async () => {
  await at({ provider: null, redirect_uri: redirect });
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  await waitFor(() => expect(window.location.pathname).toBe("/access"));

  fireEvent.click(await screen.findByRole("button", { name: "Identity provider" }));
  await screen.findByRole("dialog", { name: "Identity provider" });
  expect(window.location.pathname).toBe("/access/provider");
});

test("a Member gets no Identity provider: its path is Page not found", async () => {
  const api = fakeApi();
  actAs(api, bob);
  serve(api, world(), bob);
  renderAt("/access/provider");
  expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeDefined();
  expect(api.requests(ServerOIDCShow)).toEqual([]);
});

test("the Owner's first save needs every field, and sends all four", async () => {
  const { api, panel } = await at({ provider: null, redirect_uri: redirect });
  expect(within(panel).getByText(redirect)).toBeDefined();
  expect(within(panel).getByRole("button", { name: "Copy the redirect URI" })).toBeDefined();
  expect(within(panel).queryByRole("button", { name: "Remove" })).toBeNull();
  await expectAccessible();

  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  for (const message of ["Enter the issuer URL.", "Enter the client ID.", "Enter the client secret.", "Enter the name."]) {
    expect(await within(panel).findByText(message)).toBeDefined();
  }
  type(panel, "Issuer URL", "http://idp.example.com/");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  expect(await within(panel).findByText("The issuer URL must start with https://.")).toBeDefined();
  expect(api.requests(ServerOIDCSet)).toEqual([]);

  type(panel, "Issuer URL", authentik.issuer);
  type(panel, "Client ID", "koko");
  type(panel, "Client secret", "s3cret");
  type(panel, "Name", "Authentik");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));

  expect(await screen.findByText("Authentik saved.")).toBeDefined();
  expect(api.requests(ServerOIDCSet)).toEqual([
    { issuer: authentik.issuer, client_id: "koko", client_secret: "s3cret", name: "Authentik" },
  ]);
  // The secret is dropped as it is sent; the stored one is kept from now on.
  expect(within(panel).getByLabelText("Client secret")).toHaveProperty("value", "");
  expect(within(panel).getByText("Set. Leave the field empty to keep it.")).toBeDefined();
});

test("a save sends only what changed, and an empty secret field keeps the stored secret", async () => {
  const { api, panel } = await at({ provider: authentik, redirect_uri: redirect });
  expect(within(panel).getByLabelText("Issuer URL")).toHaveProperty("value", authentik.issuer);
  expect(within(panel).getByLabelText("Client secret")).toHaveProperty("value", "");
  expect(within(panel).getByText("Set. Leave the field empty to keep it.")).toBeDefined();
  await expectAccessible();

  type(panel, "Name", "Keycloak");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  expect(await screen.findByText("Keycloak saved.")).toBeDefined();
  type(panel, "Client secret", "n3w");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.requests(ServerOIDCSet)).toHaveLength(2));
  expect(api.requests(ServerOIDCSet)).toEqual([{ name: "Keycloak" }, { client_secret: "n3w" }]);
  // No change of issuer, so nothing asked first.
  expect(screen.queryByRole("alertdialog", { name: /^Change the issuer/ })).toBeNull();
});

test("a refusal shows in place: a provider set wrongly, or one that did not answer", async () => {
  const { api, panel } = await at({ provider: authentik, redirect_uri: redirect });
  for (const [code, status, message] of [
    ["invalid_provider", 400, "A setting of the Identity Provider is not valid, or its issuer's discovery document is not."],
    ["provider_unreachable", 502, "The Identity Provider did not answer in time. Try again later."],
  ] as const) {
    api.on(ServerOIDCSet, () => {
      throw refusal(code, status);
    });
    type(panel, "Client ID", `koko-${code}`);
    fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(alerts()).toContain(message));
  }
  expect(screen.getByRole("dialog", { name: /^Identity provider/ })).toBeDefined();
});

test("a changed issuer asks first, naming how many Linked Identities it unlinks", async () => {
  const { api, panel } = await at({ provider: authentik, redirect_uri: redirect });
  const other = "https://login.example.com/realms/koko";
  type(panel, "Issuer URL", other);
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));

  let asking = await screen.findByRole("alertdialog", { name: "Change the issuer of Authentik" });
  expect(
    within(asking).getByText(
      "The 2 Linked Identities at the current issuer are unlinked, and the browsers signed in through them are signed out. Each User links an identity again at the new issuer.",
    ),
  ).toBeDefined();
  await expectAccessible();
  fireEvent.click(within(asking).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog", { name: /^Change the issuer/ })).toBeNull());
  expect(api.requests(ServerOIDCSet)).toEqual([]);

  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  asking = await screen.findByRole("alertdialog", { name: "Change the issuer of Authentik" });
  fireEvent.click(within(asking).getByRole("button", { name: "Change issuer" }));
  expect(await screen.findByText("Authentik saved. 2 Linked Identities were unlinked.")).toBeDefined();
  expect(api.requests(ServerOIDCSet)).toEqual([{ issuer: other }]);
});

test("Remove asks first, then removes the provider and closes the panel", async () => {
  const { api, panel } = await at({ provider: authentik, redirect_uri: redirect });
  fireEvent.click(within(panel).getByRole("button", { name: "Remove" }));
  const asking = await screen.findByRole("alertdialog", { name: "Remove Authentik" });
  expect(
    within(asking).getByText(
      "The 2 Linked Identities are unlinked, and the browsers signed in through them are signed out.",
    ),
  ).toBeDefined();
  fireEvent.click(within(asking).getByRole("button", { name: "Remove" }));
  expect(await screen.findByText("Authentik removed.")).toBeDefined();
  expect(api.requests(ServerOIDCRemove)).toEqual([{}]);
  await waitFor(() => expect(window.location.pathname).toBe("/access"));
});

test("an Administrator sees the provider read-only, without its secret", async () => {
  const { api, panel } = await at({ provider: authentik, redirect_uri: redirect }, eve);
  expect(within(panel).getByText(authentik.issuer)).toBeDefined();
  expect(within(panel).getByText("Set")).toBeDefined();
  expect(within(panel).getByRole("button", { name: "Copy the redirect URI" })).toBeDefined();
  expect(within(panel).queryAllByRole("textbox")).toEqual([]);
  for (const action of ["Save", "Remove"]) {
    expect(within(panel).queryByRole("button", { name: action })).toBeNull();
  }
  await expectAccessible();
  expect(api.requests(ServerOIDCSet)).toEqual([]);
});

test("an Administrator sees that none is configured", async () => {
  const { panel } = await at({ provider: null, redirect_uri: redirect }, eve);
  expect(within(panel).getByText("No identity provider is configured.")).toBeDefined();
});

test("without a Web URL the form is disabled, saying why", async () => {
  const { api, panel } = await at({ provider: null, redirect_uri: null });
  expect(
    within(panel).getByText(
      "The redirect URI is under the Web URL, which is not set. Set it on the koko server's machine with koko server set web_url=.",
    ),
  ).toBeDefined();
  for (const label of ["Issuer URL", "Client ID", "Client secret", "Name"]) {
    expect(within(panel).getByLabelText(label)).toHaveProperty("disabled", true);
  }
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  expect(api.requests(ServerOIDCSet)).toEqual([]);
  await expectAccessible();
});

test("the panel reads in Simplified Chinese", async () => {
  preferLanguages("zh-CN");
  const { panel } = await at({ provider: authentik, redirect_uri: redirect });
  expect(within(panel).getByLabelText("签发者 URL")).toHaveProperty("value", authentik.issuer);
  expect(within(panel).getByText("已设置。留空则保留现有密钥。")).toBeDefined();
  type(panel, "签发者 URL", "https://login.example.com/");
  fireEvent.click(within(panel).getByRole("button", { name: "保存" }));
  const asking = await screen.findByRole("alertdialog", { name: "更改 Authentik 的签发者" });
  expect(within(asking).getByRole("button", { name: "更改签发者" })).toBeDefined();
  await expectAccessible();
});
