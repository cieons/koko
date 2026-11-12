import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, onTestFinished, test, vi } from "vitest";

import { type FileEntry, FilesCancel, FilesDownload, FilesFinish, FilesList, type FilesListResponse, FilesUpload } from "../api/files";
import { type Host, HostList, HostShow } from "../api/host";
import { alice, host } from "../pages/host/records.fixture";
import { navigate } from "../shell/router";
import { expectAccessible } from "../test/axe";
import { type FakeApi, fakeApi, refusal, refusalSaying } from "../test/fakeApi";
import { actAs, renderAt } from "../test/pages";
import { FakeWebSocket } from "../test/terminal";
import type { EmulatorProps } from "./emulator/Emulator";

// The Files panel, run as the whole web UI against the fake API, which
// plays the files endpoints, with a tab's SSH Session started over a fake
// WebSocket and its emulator replaced by a fake whose working directory
// (OSC 7) the test reports.

vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

const emulators = vi.hoisted(() => new Map<string, EmulatorProps>());

vi.mock("./emulator/Emulator", async () => {
  const { createElement, useEffect, useImperativeHandle } = await import("react");
  function Emulator(props: EmulatorProps) {
    emulators.set(props.label, props);
    useImperativeHandle(props.ref, () => ({
      write: () => {},
      focus: () => {},
      fit: () => {},
      selection: () => "",
      pressKey: () => {},
    }));
    useEffect(() => props.onReady(120, 36), []);
    return createElement("textarea", { "aria-label": props.label });
  }
  return { fontSizes: [12, 13, 14, 16, 18], Emulator };
});

const hosts: Host[] = [host("web1", { space: "web" }), host("db2", { space: "ops" })];

/** session is the SSH Session's id the fake koko server gives the first tab. */
const session = "0123456789abcdef0123456789abcdef";

/** chunkPath is where an upload's chunks go (api.FilesChunkPath). */
const chunkPath = "/terminal/files/chunk";

beforeEach(() => {
  FakeWebSocket.sockets = [];
  vi.stubGlobal("WebSocket", FakeWebSocket);
});

afterEach(() => {
  emulators.clear();
  sessionStorage.clear();
});

const modified = "2026-11-12T09:30:00Z";

function entry(name: string, kind: FileEntry["kind"], size = 0): FileEntry {
  return { name, kind, size, modified };
}

/** listings are the directories of web1 the fake koko server lists, by path; home is /home/alice. */
const listings: Record<string, FilesListResponse> = {
  "/home/alice": { path: "/home/alice", entries: [entry("src", "dir"), entry("notes.txt", "file", 12)], more: 0 },
  "/home/alice/src": {
    path: "/home/alice/src",
    entries: [
      entry("main.go", "file", 2048),
      entry("lib", "dir"),
      entry("current", "link"),
      entry("fifo", "other"),
      entry("README.md", "file", 512),
    ],
    more: 0,
  },
  "/home/alice/src/lib": { path: "/home/alice/src/lib", entries: [], more: 7_120 },
};

/** serve renders the web UI at path for alice, with the files endpoints answering from listings. */
function serve(path = "/terminal?host=web1"): FakeApi {
  const api = fakeApi();
  actAs(api, alice);
  api.on(HostList, () => ({ hosts }));
  api.on(HostShow, ({ host: name }) => ({ host: hosts.find((h) => h.name === name) ?? hosts[0]!, services: [] }));
  api.on(FilesList, ({ path }) => {
    const found = listings[path === "" ? "/home/alice" : path];
    if (!found) {
      throw refusalSaying("host_refused", 403, `${path}: permission denied`);
    }
    return found;
  });
  let uploads = 0;
  api.on(FilesUpload, () => ({ upload: `u${++uploads}` }));
  api.on(FilesFinish, () => ({ path: "/", size: 0 }));
  api.on(FilesCancel, () => ({}));
  api.onBytes(chunkPath, (bytes, query) => ({ received: Number(query.get("offset")) + bytes.size }));
  renderAt(path);
  return api;
}

/** sockets waits for the page to have opened n WebSockets, and is them. */
async function sockets(n: number): Promise<FakeWebSocket[]> {
  await waitFor(() => expect(FakeWebSocket.sockets).toHaveLength(n));
  return [...FakeWebSocket.sockets];
}

/** connected starts web1's shell as the SSH Session `session`, its shell in cwd when given (OSC 7), and is its WebSocket. */
async function connected(cwd?: string): Promise<FakeWebSocket | undefined> {
  const [socket] = await sockets(1);
  await act(async () => {
    socket?.opened();
    socket?.answer({ tunnel: session });
  });
  await waitFor(() => expect(screen.getByRole("tab", { name: /web1/ }).textContent).not.toContain("Connecting"));
  if (cwd !== undefined) {
    act(() => emulators.get("Terminal on web1")?.onWorkingDirectory(cwd));
  }
  return socket;
}

/** press is a pointer's press on el, as a browser sends it. */
function press(el: Element): void {
  fireEvent.pointerDown(el, { pointerType: "mouse" });
  fireEvent.mouseDown(el);
  fireEvent.pointerUp(el, { pointerType: "mouse" });
  fireEvent.mouseUp(el);
  fireEvent.click(el);
}

/** openFiles presses Files, and is the column it opens. */
function openFiles(): HTMLElement {
  press(screen.getByRole("button", { name: "Files" }));
  return screen.getByRole("complementary", { name: "Files" });
}

/** listed waits for the panel to list the directory at path, and is its entries' buttons by their names. */
async function listed(column: HTMLElement, path: string): Promise<string[]> {
  const entries = await within(column).findByRole("list", { name: `Contents of ${path}` });
  return within(entries)
    .queryAllByRole("button")
    .map((row) => row.getAttribute("aria-label") ?? "");
}

/** pick chooses files in the panel's Upload, as the browser's file chooser hands them over. */
function pick(column: HTMLElement, ...files: File[]): void {
  const input = column.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) {
    throw new Error("the panel has no file input");
  }
  fireEvent.change(input, { target: { files } });
}

/** file is a file of size bytes. */
const file = (name: string, size: number) => new File([new Uint8Array(size)], name);

/** uploadRow is the upload row of that file name. */
function uploadRow(column: HTMLElement, name: string): HTMLElement {
  const uploads = within(column).getByRole("region", { name: "Uploads" });
  const row = within(uploads)
    .getAllByRole("listitem")
    .find((item) => item.querySelector("p")?.textContent === name);
  if (!row) {
    throw new Error(`no upload of ${name} is listed`);
  }
  return row;
}

test("the panel opens where the shell is, enters a directory, goes up, and downloads a file through a link with its token", async () => {
  const api = serve();
  await connected("file:///home/alice/src");
  const column = openFiles();

  // Directories first, then files, by name in the browser's collation.
  expect(await listed(column, "/home/alice/src")).toEqual([
    "Open lib",
    "Open current",
    "Download fifo",
    "Download main.go",
    "Download README.md",
  ]);
  expect(api.requests(FilesList)).toEqual([{ tunnel: session, path: "/home/alice/src" }]);
  // Something that is neither a file nor a directory, such as a FIFO, is shown and not offered.
  expect(within(column).getByRole("button", { name: "Download fifo" }).hasAttribute("disabled")).toBe(true);
  expect(within(column).getByRole("button", { name: "Download main.go" }).textContent).toContain("2 KiB");
  await expectAccessible();

  // A directory opens; the 5,000-entry note says how many more there are.
  fireEvent.click(within(column).getByRole("button", { name: "Open lib" }));
  expect(await listed(column, "/home/alice/src/lib")).toEqual([]);
  expect(within(column).getByText("This directory is empty.")).toBeDefined();
  expect(within(column).getByText("7120 more entries are not listed.")).toBeDefined();
  await expectAccessible();

  // Up goes to the directory above.
  press(within(column).getByRole("button", { name: "Up one directory" }));
  await listed(column, "/home/alice/src");
  expect(api.requests(FilesList).at(-1)).toEqual({ tunnel: session, path: "/home/alice/src" });

  // A file downloads through a link to its token, which the browser follows.
  const followed: { href: string | null; download: boolean }[] = [];
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
    followed.push({ href: this.getAttribute("href"), download: this.hasAttribute("download") });
  });
  onTestFinished(() => click.mockRestore());
  const token = "00112233445566778899aabbccddeeff";
  api.on(FilesDownload, ({ path }) => ({ token, name: path.split("/").at(-1) ?? "", size: 2048 }));
  fireEvent.click(within(column).getByRole("button", { name: "Download main.go" }));
  await waitFor(() => expect(followed).toEqual([{ href: `/terminal/files/download/${token}`, download: true }]));
  expect(api.requests(FilesDownload)).toEqual([{ tunnel: session, path: "/home/alice/src/main.go" }]);
  // The link does not stay in the page.
  expect(document.querySelector(`a[href$="${token}"]`)).toBeNull();

  // A link to a directory is entered: its download is refused as not a file.
  api.on(FilesDownload, () => {
    throw refusal("not_a_file", 409);
  });
  api.on(FilesList, ({ path }) => ({ path, entries: [], more: 0 }));
  fireEvent.click(within(column).getByRole("button", { name: "Open current" }));
  await listed(column, "/home/alice/src/current");
});

test("the panel opens on home when the shell reported no directory, another machine's, or one that cannot be listed", async () => {
  const api = serve();
  const socket = await connected();
  const column = openFiles();
  await listed(column, "/home/alice");
  expect(api.requests(FilesList)).toEqual([{ tunnel: session, path: "" }]);
  press(within(column).getByRole("button", { name: "Close Files" }));

  // A directory on another machine is not this Host's.
  act(() => emulators.get("Terminal on web1")?.onWorkingDirectory("file://laptop/home/alice/src"));
  openFiles();
  await listed(screen.getByRole("complementary", { name: "Files" }), "/home/alice");
  expect(api.requests(FilesList).at(-1)).toEqual({ tunnel: session, path: "" });
  press(screen.getByRole("button", { name: "Close Files" }));

  // A directory the Host refuses to list gives way to home.
  act(() => emulators.get("Terminal on web1")?.onWorkingDirectory("file://web1/root"));
  openFiles();
  await listed(screen.getByRole("complementary", { name: "Files" }), "/home/alice");
  expect(api.requests(FilesList).slice(-2)).toEqual([
    { tunnel: session, path: "/root" },
    { tunnel: session, path: "" },
  ]);
  expect(socket).toBeDefined();
});

test("a directory the Host refuses says so in the Host's words, with Try again", async () => {
  const api = serve();
  await connected("file:///home/alice/src");
  const column = openFiles();
  await listed(column, "/home/alice/src");
  let refused = true;
  api.on(FilesList, ({ path }) => {
    if (refused) {
      throw refusalSaying("host_refused", 403, `${path}: permission denied`);
    }
    return listings[path]!;
  });
  fireEvent.click(within(column).getByRole("button", { name: "Open lib" }));
  const alert = await within(column).findByRole("alert");
  expect(alert.textContent).toContain("The Host refused the request.");
  expect(alert.textContent).toContain("/home/alice/src/lib: permission denied");
  await expectAccessible();
  refused = false;
  press(within(column).getByRole("button", { name: "Try again" }));
  await listed(column, "/home/alice/src/lib");
  expect(within(column).queryByRole("alert")).toBeNull();
});

test("uploads go in 512 KiB chunks with their progress, show in the directory, and stay listed from another tab", async () => {
  const api = serve();
  await connected("file:///home/alice/src");
  const column = openFiles();
  await listed(column, "/home/alice/src");
  const held: (() => void)[] = [];
  api.onBytes(
    chunkPath,
    (bytes, query) =>
      new Promise((resolve) => held.push(() => resolve({ received: Number(query.get("offset")) + bytes.size }))),
  );

  pick(column, file("big.bin", 600 << 10));
  await waitFor(() => expect(held).toHaveLength(1));
  expect(api.requests(FilesUpload)).toEqual([
    { tunnel: session, dir: "/home/alice/src", name: "big.bin", size: 600 << 10, replace: false },
  ]);
  const row = uploadRow(column, "big.bin");
  expect(row.textContent).toContain("To web1: /home/alice/src");
  expect(within(row).getByRole("progressbar", { name: "big.bin" }).getAttribute("aria-valuenow")).toBe("0");
  await expectAccessible();

  // The first chunk is 512 KiB, the second the rest.
  act(() => held.shift()?.());
  await waitFor(() => expect(held).toHaveLength(1));
  const chunks = api.calls.filter((call) => call.path === chunkPath);
  expect(chunks.map((call) => [call.query.get("upload"), call.query.get("offset"), (call.request as Blob).size])).toEqual([
    ["u1", "0", 512 << 10],
    ["u1", String(512 << 10), 88 << 10],
  ]);
  expect(chunks[0]?.headers["content-type"]).toBe("application/octet-stream");
  await waitFor(() => expect(row.textContent).toContain("512 KiB of 600 KiB"));
  expect(within(row).getByRole("progressbar").getAttribute("aria-valuenow")).toBe(String(512 << 10));

  // Finished, it shows in the directory, which is listed again.
  const lists = api.requests(FilesList).length;
  act(() => held.shift()?.());
  await waitFor(() => expect(uploadRow(column, "big.bin").textContent).toContain("Uploaded"));
  expect(api.requests(FilesFinish)).toEqual([{ upload: "u1" }]);
  await waitFor(() => expect(api.requests(FilesList)).toHaveLength(lists + 1));

  // Another tab's panel lists it too, until it is dismissed.
  act(() => navigate("/hosts/db2/terminal"));
  await sockets(2);
  await waitFor(() => expect(screen.getByRole("complementary", { name: "Files" }).textContent).toContain("on db2"));
  const other = screen.getByRole("complementary", { name: "Files" });
  expect(uploadRow(other, "big.bin").textContent).toContain("To web1: /home/alice/src");
  press(within(other).getByRole("button", { name: "Dismiss big.bin" }));
  expect(within(other).queryByRole("region", { name: "Uploads" })).toBeNull();
});

test("past four uploads at once the rest wait their turn, and Cancel stops one and removes its partial file", async () => {
  const api = serve();
  await connected("file:///home/alice/src");
  const column = openFiles();
  await listed(column, "/home/alice/src");
  const held: (() => void)[] = [];
  api.onBytes(chunkPath, (bytes) => new Promise((resolve) => held.push(() => resolve({ received: bytes.size }))));

  pick(column, file("a", 1), file("b", 1), file("c", 1), file("d", 1), file("e", 1), file("f", 1));
  await waitFor(() => expect(held).toHaveLength(4));
  expect(api.requests(FilesUpload)).toHaveLength(4);
  expect(uploadRow(column, "e").textContent).toContain("Waiting");
  expect(uploadRow(column, "f").textContent).toContain("Waiting");
  await expectAccessible();

  // A waiting one cancelled never starts.
  press(within(uploadRow(column, "e")).getByRole("button", { name: "Cancel the upload of e" }));
  expect(uploadRow(column, "e").textContent).toContain("Cancelled");

  // A running one cancelled: the koko server is told, which removes its
  // partial file, and its place goes to the next waiting once its chunk is
  // answered.
  press(within(uploadRow(column, "a")).getByRole("button", { name: "Cancel the upload of a" }));
  expect(uploadRow(column, "a").textContent).toContain("Cancelled");
  await waitFor(() => expect(api.requests(FilesCancel)).toEqual([{ upload: "u1" }]));
  expect(api.requests(FilesUpload)).toHaveLength(4);
  act(() => held.shift()?.());
  await waitFor(() => expect(api.requests(FilesUpload)).toHaveLength(5));
  expect(api.requests(FilesUpload)[4]).toMatchObject({ name: "f" });

  await waitFor(() => expect(held).toHaveLength(4));
  for (const release of held.splice(0)) {
    act(release);
  }
  await waitFor(() => expect(uploadRow(column, "f").textContent).toContain("Uploaded"));
  await waitFor(() => expect(api.requests(FilesFinish)).toHaveLength(4));
  expect(api.requests(FilesFinish).map((r) => (r as { upload: string }).upload).sort()).toEqual(["u2", "u3", "u4", "u5"]);
  expect(api.requests(FilesUpload).map((r) => (r as { name: string }).name)).not.toContain("e");
  press(within(uploadRow(column, "a")).getByRole("button", { name: "Dismiss a" }));
  expect(() => uploadRow(column, "a")).toThrow("no upload of a is listed");
});

test("signing out forgets the uploads, as it ends the tabs", async () => {
  const api = serve();
  await connected("file:///home/alice");
  const column = openFiles();
  await listed(column, "/home/alice");
  api.onBytes(chunkPath, () => new Promise(() => {}));
  pick(column, file("a", 1));
  await waitFor(() => expect(uploadRow(column, "a").textContent).toContain("0 B of 1 B"));

  // Signing out leaves the frame; the next Sign-in's terminal has no upload of the last.
  act(() => navigate("/signin"));
  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  act(() => navigate("/terminal?host=web1"));
  await sockets(2);
  const again = openFiles();
  expect(within(again).queryByRole("region", { name: "Uploads" })).toBeNull();
});

test("a file whose name is in the directory is asked about first, then sent to replace it", async () => {
  const api = serve();
  await connected("file:///home/alice");
  const column = openFiles();
  await listed(column, "/home/alice");

  pick(column, file("notes.txt", 3), file("new.txt", 3), file("src", 1));
  // The new name goes at once; the others wait for their answers, one at a time.
  await waitFor(() => expect(api.requests(FilesUpload)).toHaveLength(1));
  expect(api.requests(FilesUpload)[0]).toMatchObject({ name: "new.txt", replace: false });
  const ask = await screen.findByRole("alertdialog", { name: "Replace notes.txt in /home/alice?" });
  expect(ask.textContent).toContain("The file on web1 is replaced by the one uploaded, and its present content is lost.");
  await expectAccessible();
  press(within(ask).getByRole("button", { name: "Replace" }));
  await waitFor(() => expect(api.requests(FilesUpload)).toHaveLength(2));
  expect(api.requests(FilesUpload)[1]).toEqual({ tunnel: session, dir: "/home/alice", name: "notes.txt", size: 3, replace: true });

  const second = await screen.findByRole("alertdialog", { name: "Replace src in /home/alice?" });
  press(within(second).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(api.requests(FilesUpload)).toHaveLength(2);
});

test("a chunk whose connection failed is sent again, from where the koko server has come to", async () => {
  const api = serve();
  await connected("file:///home/alice");
  const column = openFiles();
  await listed(column, "/home/alice");
  let failures = 1;
  let resume: (() => void) | undefined;
  api.onBytes(chunkPath, async (bytes, query) => {
    if (failures-- > 0) {
      throw new TypeError("Failed to fetch");
    }
    await new Promise<void>((resolve) => (resume = resolve));
    return { received: Number(query.get("offset")) + bytes.size };
  });

  pick(column, file("log.txt", 10));
  await waitFor(() => expect(uploadRow(column, "log.txt").textContent).toContain("The connection dropped. Sending again…"));
  await expectAccessible();
  await waitFor(() => expect(resume).toBeDefined());
  act(() => resume?.());
  await waitFor(() => expect(uploadRow(column, "log.txt").textContent).toContain("Uploaded"));
  expect(api.calls.filter((call) => call.path === chunkPath).map((call) => call.query.get("offset"))).toEqual(["0", "0"]);

  // A refusal is no dropped connection: the upload fails, and says why.
  api.onBytes(chunkPath, () => {
    throw refusal("ssh_session_ended", 410);
  });
  pick(column, file("other.txt", 10));
  await waitFor(() =>
    expect(uploadRow(column, "other.txt").textContent).toContain("This SSH Session has ended, and its file transfers with it."),
  );
  await expectAccessible();
});

test("files dropped on the terminal upload into the directory its shell is in", async () => {
  const api = serve();
  await connected("file:///home/alice/src");
  const terminal = screen.getAllByRole("tabpanel").find((panel) => !panel.hasAttribute("inert"))?.parentElement;
  if (!terminal) {
    throw new Error("no terminal is shown");
  }
  const dropped = [file("main.go", 4), file("new.go", 4)];
  const dataTransfer = { types: ["Files"], files: dropped, dropEffect: "none" };
  fireEvent.dragEnter(terminal, { dataTransfer });
  expect(screen.getByText("Drop to upload into /home/alice/src on web1")).toBeDefined();
  fireEvent.drop(terminal, { dataTransfer });
  expect(screen.queryByText("Drop to upload into /home/alice/src on web1")).toBeNull();

  // Files opens on that directory; a name it holds is asked about first.
  const column = screen.getByRole("complementary", { name: "Files" });
  await listed(column, "/home/alice/src");
  await waitFor(() => expect(api.requests(FilesUpload)).toHaveLength(1));
  expect(api.requests(FilesUpload)[0]).toMatchObject({ dir: "/home/alice/src", name: "new.go", replace: false });
  const ask = await screen.findByRole("alertdialog", { name: "Replace main.go in /home/alice/src?" });
  press(within(ask).getByRole("button", { name: "Replace" }));
  await waitFor(() => expect(api.requests(FilesUpload)).toHaveLength(2));
  expect(api.requests(FilesUpload)[1]).toMatchObject({ name: "main.go", replace: true });
});

test("a Host without SFTP says so as one panel, and a tab whose SSH Session has ended lists nothing", async () => {
  const api = serve();
  api.on(FilesList, () => {
    throw refusal("sftp_unavailable", 409);
  });
  const socket = await connected("file:///home/alice");
  const column = openFiles();
  await within(column).findByText("web1 offers no SFTP, which koko moves files through.");
  expect(within(column).getByText("The terminal is unaffected.")).toBeDefined();
  expect(within(column).queryByRole("button", { name: "Upload" })).toBeNull();
  // Home is not tried: no directory of this Host can be listed.
  expect(api.requests(FilesList)).toHaveLength(1);
  await expectAccessible();

  await act(async () => {
    socket?.records({ type: "exitStatus", status: 0 });
    socket?.frames({ kind: "close", ack: 0, reason: "" });
  });
  await within(column).findByText("The SSH Session has ended.");
  await expectAccessible();
});
