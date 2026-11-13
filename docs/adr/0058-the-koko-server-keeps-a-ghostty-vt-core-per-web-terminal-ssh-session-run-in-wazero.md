# The koko server keeps a Ghostty VT core per web terminal SSH Session, run in wazero

Each web terminal SSH Session has an emulator in the koko server, so that a
page attaching to it gets an exact picture of the terminal (ADR 0057, spec
#213). The emulator is Ghostty's VT core: libghostty-vt's WebAssembly module,
run by [wazero](https://github.com/tetratelabs/wazero), a WebAssembly runtime
in pure Go (Apache-2.0, with no dependencies of its own). `kokod` stays one
binary without cgo (ADR 0001, ADR 0040). The package is
`internal/server/vt`.

- **The module** is libghostty-vt 1.0.1's released `ghostty-vt-1.0.1-small.wasm`,
  as published at `release.files.ghostty.org`. It is not a build of koko's
  own. It is committed beside the package and pinned by its SHA-256 in
  `ghostty-vt.wasm.sum`, which also names the release file. The koko server
  checks the embedded file against the pin before it compiles it, and
  `make vt-wasm` fetches the file again and checks it. Ghostty's MIT licence is committed beside
  it. The small build is pinned rather than the fast one: both wrote about
  180 MB/s of mixed coloured output on one core, and the small build is
  845 KB where the fast build is 1.2 MB.
- **wazero's compiler** runs the module. The module is compiled once per
  process, in about 150 ms, and each SSH Session gets an instance of its own,
  made in about 1 ms. The compiler writes machine code to memory and then
  executes it. wazero's interpreter, which needs neither, was about 100
  times slower (1.7 MB/s), so the systemd unit `kokod` installs never sets
  `MemoryDenyWriteExecute=` or a system call filter, and a test of the
  installer fails if it does. If the system refuses anyway, as SELinux's
  `deny_execmem` can, the interpreter runs, and `vt.Engine` says which
  engine runs, and why, for the koko server to log at its start. The package's tests, with the compiler, passed
  on linux/arm64 in Docker, in the image's distroless base and under the
  unit's restrictions: a user other than root, no capabilities, no new
  privileges and a read-only root. A real systemd unit and linux/amd64 were
  not run there; `make check` on CI's Linux runner runs the tests on
  linux/amd64.
- **The callbacks.** Ghostty takes its callbacks, such as the replies to
  queries, as indices in its WebAssembly function table, and a Go function
  can reach that table only through a WebAssembly module. koko's bridge is
  such a module, a few dozen bytes written out in Go. It imports Ghostty's
  table and koko's four functions, and appends them to the table. Each
  instance's bridge is bound to that instance through wazero's experimental
  import resolver, since the instances have no names.
- **The snapshot on attach** is VT bytes that draw the terminal on a cleared
  emulator of its size:
  - Ghostty's formatter writes the active screen with its history, every row
    and every wrapped line whole, and the modes, scroll region, tab stops,
    cursor, pen, keyboard modes and character sets. It writes the modes
    before the screen, so autowrap stays on while it runs and is turned off
    after the screen, where a program turned it off.
  - The formatter reaches only the active screen. While the alternate
    screen is active, Snapshot first copies the terminal through Ghostty's
    own snapshot format into an instance of its own. It leaves the
    alternate screen in the copy as the program would, writes the copy's
    primary screen with what leaving restored (the saved cursor, the
    cursor, its shape, the pen and the Kitty keyboard flags), and closes
    the instance, so the SSH Session's own memory does not grow by the
    copy. No export of koko's own was needed. The copy is kept while the
    alternate screen stays, since Ghostty's decoder takes up to a second on
    a screen of combining marks.
  - Each screen's saved cursor (DECSC) is read back from a copy decoded only
    as far as its screens, once the output has saved one, and written with
    `ESC 7` before the screen's content, which never touches it.
  - Snapshot adds what the formatter leaves out: the colours a program set
    (OSC 10, 11 and 12), the cursor's shape, the working directory (the formatter's OSC 7 carries a stray NUL), the
    title (OSC 2) and an escape sequence the output left unfinished, so
    the page takes the rest of it as the core does. The shape is asked with
    DECRQSS while the output is between sequences, twice with two default
    shapes. A shape that does not change with the default is one a program
    set, and only that is restored.
- **The palette is kept as indices.** The formatter's palette extra is off,
  so `38;5;n` and the 16 ANSI colours reach the page as indices, and the
  page's theme colours them in Light and Dark. A palette entry is written
  (OSC 4) only where it differs from the core's default, which is only
  where a program set it. A query of a palette entry (OSC 4) is answered
  from Ghostty's default palette, not the page's theme.
- **The history** is at least 1,000 lines (`Scrollback`). Ghostty drops
  history a whole page at a time, once it passes its line limit, and a page
  holds more rows the narrower the grid. So the limit is 1,000 lines and
  two pages' rows more: about 1,130 lines at 500 columns and 1,540 at 120.
  That costs about two pages of memory, under 0.5 MiB.
- **The queries the koko server answers.** These are device attributes, with
  Ghostty's default reply as a VT220 with colour (`CSI ? 62 ; 22 c`), the
  cursor's place, mode reports, OSC 10 and 11 with the colours `SetColors`
  last gave, and the window's size (`CSI 14 t`, `CSI 18 t`) as the pty's
  cells and 0 pixels. They also include the in-band size report on a resize
  that a program asked for (mode 2048). The core keeps no Kitty images.
  The released module leaves them out, and the page draws text alone.
- **The largest grid** is 500×200 (`vt.MaxCols`, `vt.MaxRows`, the spec's
  `maxGrid`): a terminal holds a larger size to it.
- **Memory, and failing.** With ordinary output and its history full, a
  core of 120×40 holds about 3 to 5 MiB. One of 500×200 holds 7 to 9 MiB,
  and up to 76 MiB when an image viewer fills every cell with truecolour
  ([the reference](../subsystems/vt.md#memory) has the measurements). A
  core past 96 MiB (`memoryBound`) has failed. Such cores are floods of
  combining marks or of long hyperlinks, which are not ordinary output.
  wazero caps each instance at 128 MiB, above any one call's growth, so
  Ghostty's own allocations never fail inside a call. A User's 16 tabs
  hold about 90 MiB with ordinary output, and at most 1.5 to 2 GiB.
- **A failed Terminal stays failed.** Any error closes the instance, and
  every later call returns the same error. Errors include a trap, a
  cancelled context, an address out of range and a core past its bound.
  Ghostty's state after one is not to be trusted. Cancelling the context a
  Terminal was made with ends a Write within its next 4 KiB, and any other
  call at its next step into the core. wazero's `WithCloseOnContextDone`
  would end a call inside Ghostty too, but it cut writes from 180 to
  19.5 MB/s. A loop in Ghostty that never ends is therefore not stopped;
  the slowest output seen, DECALN at 500×200, takes 0.15 s per 4 KiB.

## Considered Options

- **A build of Ghostty's source of koko's own, with an export for the
  primary screen behind the alternate one:** this was the spec's expected
  route. But copying the terminal through the snapshot format gives the
  same screen from the released file, and a build of koko's own would need
  Zig 0.17 in koko's toolchain and a pin of its own.
- **go-headless-term with a serialiser of koko's own:** the fallback, pure
  Go, but about 58 MiB per SSH Session at 5,000 lines, with the serialiser
  to write and keep. It was not needed.
- **`charmbracelet/x/vt`:** pure Go, but about 72 MiB per SSH Session at
  5,000 lines, too slow for plain text, with its modes unexported, and its
  `Write` blocks for good on a device attributes query unless its replies
  are read.
- **`mitchellh/go-libghostty`:** Ghostty's formatter in Go, but through
  cgo, which `kokod`'s build does not use.
- **A snapshot built from wterm's build's cells:** no new WebAssembly, but
  its cells carry colours resolved to RGB, so the 16 ANSI colours would stop
  following Light and Dark.
- **Replaying the last raw output and forcing a redraw:** no emulator, but
  full-screen programs come back garbled once the switch to their screen
  falls outside the buffer. Modes such as bracketed paste are lost, old
  queries are answered again into the shell, and output recorded at an
  older size wraps wrongly.
- **Ghostty's render state for the cursor's shape:** its one getter, but an
  update copies the whole screen. A core of 500×200 grew by 20 MiB, for
  good, to read one value.
- **The fast build of the module:** no faster on koko's output, and 0.4 MB
  larger.
- **A history byte limit against floods:** it did not bound a flood of long
  hyperlinks, and it would keep fewer than 1,000 lines of heavy but
  ordinary output.

## Consequences

- `kokod` grows by 0.85 MB, and koko gains one Go dependency,
  `github.com/tetratelabs/wazero` v1.12.0. Its experimental import
  resolver may change in a later wazero, which a wazero upgrade's build
  and tests show.
- Upgrading the core is a new line in `ghostty-vt.wasm.sum`, then
  `make vt-wasm` and the tests. The page's core is wterm's build of Ghostty
  1.3.1 (ADR 0054) and the koko server's is libghostty-vt 1.0.1, so a
  Vitest test writes each case's snapshot into the real wterm and compares
  the screen with the shell's output written into it directly.
- The koko server answers queries, and the page answers none, so they are
  answered once however many pages are attached. This holds while wterm
  gives its replies within `write`, which a Vitest test checks for wterm
  0.5.10. wterm gives none to `CSI 18 t`.
- A shape set by a program while its output was cut in the middle of a
  sequence at the moment of a snapshot is left out of that snapshot. The
  page shows its default cursor until the program sets the shape again.
