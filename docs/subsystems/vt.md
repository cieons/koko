# The emulator in the koko server

Each web terminal SSH Session's emulator in the koko server ([ADR
0058](../adr/0058-the-koko-server-keeps-a-ghostty-vt-core-per-web-terminal-ssh-session-run-in-wazero.md),
spec #213): Ghostty's VT core, libghostty-vt's WebAssembly module, run by
wazero. One package, [`vt`](../../internal/server/vt/vt.go), knows Ghostty and
wazero. It takes the shell's output, answers the queries in it, and gives a
snapshot that draws the terminal on a page's cleared emulator. Nothing
outside the package uses it yet; the Gateway's SSH Session loop will own one
`Terminal` per SSH Session.

## The interface

| Call | What it does |
| --- | --- |
| `New(ctx, cols, rows)` | A terminal with at least `Scrollback` (1,000) lines of history. A size past `MaxCols` by `MaxRows` (500×200, the spec's `maxGrid`) is held to it. |
| `Write(p)` | Takes the shell's output, and returns the replies to the queries it held, for the shell's stdin. On an error, it returns the replies to the output taken before the error. |
| `Resize(cols, rows)` | Resizes, held to `MaxCols` by `MaxRows`, reflowing the primary screen, and returns any reply, such as the in-band size report (mode 2048). |
| `Size()` | The size as held. |
| `SetColors(fg, bg)` | The colours OSC 10 and 11 answer with. |
| `Snapshot()` | VT bytes that draw the terminal on a cleared emulator of its size ([below](#the-snapshot)). |
| `Close()` | Frees the instance and its memory. |
| `Engine()` | Which of wazero's engines runs the module, for the koko server to log once at start. |

- **One goroutine uses a `Terminal`.** Another may only cancel the context
  given to `New`. Cancelling ends a `Write` within its next 4 KiB and any
  other call at its next step into the core. A call under way inside
  Ghostty cannot be stopped: wazero's `WithCloseOnContextDone` could stop
  one, but it cut writes from 180 to 19.5 MB/s. The slowest output seen,
  screen alignment tests (DECALN) at 500×200, takes about 0.15 s per 4 KiB.
- **A failed `Terminal` stays failed.** Any error closes the instance, and
  every later call returns the same error. Errors include:
  - a trap;
  - a cancelled context;
  - an address past the instance's memory;
  - a core past `memoryBound`.

  After an error, the caller makes a new `Terminal`.

## The module and the runtime

- **The module** is [`ghostty-vt.wasm`](../../internal/server/vt/ghostty-vt.wasm),
  libghostty-vt's release file named in
  [`ghostty-vt.wasm.sum`](../../internal/server/vt/ghostty-vt.wasm.sum) with
  its SHA-256. It is embedded in `kokod`, and `loadEngine` checks it against
  the pin before compiling it.
  - `make vt-wasm` fetches it from `release.files.ghostty.org` and checks it.
  - Pinning a new release starts with verifying its `SHA256SUMS` against
    Ghostty's minisign key, as [Ghostty's install
    page](https://lib.ghostty.org/vt/installing.html) shows. The new line
    then goes in the `.sum` file, followed by `make vt-wasm` and the tests,
    the Vitest golden test among them.
  - The module imports nothing. [`abi.go`](../../internal/server/vt/abi.go)
    holds the parts of its C API that koko calls, as wasm32 sees them.
- **The engine** ([`engine.go`](../../internal/server/vt/engine.go)) is one
  wazero runtime per process, made on the first `New`.
  - It compiles the module in about 150 ms. Each `Terminal` is an instance,
    made in about 1 ms.
  - wazero caps each instance at 128 MiB (`memoryLimitPages`). A core fails
    first, at `memoryBound`, 96 MiB, after the call that passed it, so
    Ghostty's own allocations never fail at the cap.
- **The bridge.** Ghostty takes callbacks as function table indices.
  - A module of koko's own (`bridgeModule`) imports the instance's table and
    the four functions of the host module `koko`:
    - `write_pty`, for replies;
    - `size`, which gives the pty's cells and 0 pixels;
    - `write` and `read`, which stream a `GhosttyWriter`'s output and a
      `GhosttyReader`'s input.
  - Its `install` appends them to the table.
  - Each call into the instance carries its `Terminal` in the context, where
    the callbacks find it. An address out of range panics in a callback,
    which wazero returns as the call's error.
- **The compiler.** wazero's compiler writes machine code to memory and then
  executes it, and the interpreter is about 100 times slower. `loadEngine`
  tries the compiler first and falls back to the interpreter; `Engine` says
  which runs, and why. The unit sets neither `MemoryDenyWriteExecute=` nor
  `SystemCallFilter=` (`TestTheUnitLetsTheEmulatorCompile`).

## A terminal's settings

`configure` gives every core the same settings:

- **The history limit** is `Scrollback` and two pages' rows more
  (`historyLimit`), since Ghostty drops history a whole page at a time. The
  byte limit is off, since its default would keep one page. At 120 columns a
  terminal keeps from about 1,160 to 1,540 lines.
- **No Kitty images:** the released module leaves them out, and the page
  draws text alone. An APC sequence is buffered up to 64 KiB.
- **The unfinished sequence** is kept up to 1 MiB, for the snapshot. A
  longer one is left out of the snapshot, and the page prints its rest as
  text.

## The snapshot

[`Snapshot`](../../internal/server/vt/snapshot.go) writes, in order:

1. **The primary screen behind the alternate one**, when the alternate
   screen is active.
   - It carries its history and what leaving the alternate screen restores:
     the saved cursor, the cursor, its shape, the pen and the Kitty keyboard
     flags, which each screen keeps apart.
   - The terminal is copied through Ghostty's snapshot format into an
     instance of its own. The copy leaves the alternate screen with the mode
     that entered it (1049, 1047 or 47), and the formatter writes the copy's
     screen. The instance is closed with the copy, so the terminal's own
     memory does not grow by it.
   - The result is kept (`primary`) while the alternate screen stays.
     Output that may leave it drops the kept copy, as does a resize. Such
     output holds `47`, `1049` or a full reset, matched across the end of
     one `Write` and the start of the next.
2. **The switch to the alternate screen**, when it is active, in the mode
   that entered it.
3. **The active screen's saved cursor (DECSC) and pen, then `ESC 7`**, once
   the output has saved a cursor (`ESC 7`, `CSI s` or mode 1048).
   - Ghostty keeps a saved cursor for each screen and gives it to no
     getter. A copy decoded only as far as its screens (the decoder's READY
     step) restores it, and the formatter writes it.
   - A screen's content never touches its saved cursor, so it goes before
     the content.
4. **The active screen from Ghostty's formatter**, with its history, every
   row and every wrapped line whole.
   - The modes come first, then the screen. Then come the scroll region,
     the tab stops, the keyboard modes, the cursor's place, the pen, the
     character sets and the protection.
   - Two kinds of mode are held while the formatter runs, and given back
     after:
     - the alternate screen's, which step 2 wrote;
     - autowrap, which is on, since a wrapped line written whole would
       replay cut without it. Where a program turned autowrap off, the
       snapshot turns it off again after the screen.
5. **What the formatter leaves out:**
   - the palette entries that differ from the default (OSC 4);
   - the foreground, background and cursor colours a program set (OSC 10,
     11 and 12), apart from the defaults `SetColors` gave;
   - the cursor's shape (DECSCUSR), when a program set one;
   - the working directory (OSC 7);
   - the title (OSC 2);
   - an escape sequence or UTF-8 character the output left unfinished.

Colours stay palette indices: the formatter's palette extra is off. A query
of a palette entry (OSC 4) is answered from Ghostty's default palette, not
the page's theme.

The cursor's shape is asked with DECRQSS twice, with two default shapes,
while the output is between sequences. A shape that does not change with
the default is one a program set. The blinking mode, which the second
default changes, is set back.

If a copy fails, what it gives is left out, and the page still attaches.

**Cost.** A snapshot changes nothing in the terminal. Of 1,000 lines of
typical coloured output at 120×40 it is about 130 KB, taken in about 2 ms.

| Output | Grid | Without vim | Over vim, first | Over vim, kept |
| --- | --- | --- | --- | --- |
| Ordinary coloured output | 500×200 | 8 ms | 20 ms | 0 ms |
| Ordinary, with a saved cursor | 500×200 | 15 ms | 23 ms | 7 ms |
| Combining marks in every cell | 500×200 | 38 ms | 0.8 s | 0 ms |
| Combining marks, with a saved cursor | 500×200 | 0.24 s | 0.9 s | 0.2 s |

Ghostty's decoder is slow on combining marks, which ordinary output does not
fill a screen with.

## Memory

Measured with ordinary output, the history written past its limit:

| Output | Grid | Idle | History full | After resizes and snapshots over vim | Go heap |
| --- | --- | --- | --- | --- | --- |
| 256 colours, one per cell | 120×40 | 1.3 MiB | 2.9 MiB | 4.6 MiB | 5.5 MiB |
| 256 colours, one per cell | 500×200 | 2.1 MiB | 7.3 MiB | 8.6 MiB | 10.6 MiB |
| Truecolour fore- and background in every cell, as image viewers write | 120×40 | 1.3 MiB | 23.8 MiB | 29.4 MiB | 31.6 MiB |
| The same | 500×200 | 2.1 MiB | 67.1 MiB | 75.6 MiB | 77.5 MiB |

- **What the columns measure.** The first three are the instance's linear
  memory. The Go heap column adds wazero's structures and the kept primary
  screen.
- **Memory never shrinks.** An instance's memory stays at its peak, so the
  fourth column is what an SSH Session holds once it has resized and a page
  has attached over vim.
- **The kept primary screen** is the formatted primary screen, 0.1 MB for
  typical output and about 23 MB for the last row.
- **Sixteen terminals**, of 120×40 with the history full and a snapshot over
  vim, held 106 MiB in all and gave it back on `Close`
  (`TestSixteenTerminalsAreFreedOnClose` logs the figure).
- **Floods that are not ordinary output** pass `memoryBound` and fail the
  Terminal: combining marks reached 83 MiB at 500×200, and 2 KB hyperlinks
  in every cell reached 96 MiB in about a second at 120×40. A history byte
  limit did not bound the hyperlinks.
- **A User's 16 tabs** hold about 90 MiB with ordinary output at 120×40.
  With image viewers in every tab at 500×200 they hold about 1.6 GiB. The
  most they can hold is 16 cores at 96 MiB, each reaching up to 128 MiB
  within a call: 1.5 to 2 GiB.
- **Speed.** The core writes about 180 MB/s of mixed coloured output on one
  core (`BenchmarkWrite`).

## Verify

`go test ./internal/server/vt` (about 3 s; 10 s under `-race`):

- **`TestASnapshotDrawsTheSameTerminal`.** Each case is written into one core,
  and its snapshot into a fresh core of the same size.
  - The two are compared cell by cell through Ghostty's grid getters, not its
    formatter: text, width, style with palette and RGB apart, and the row's
    wrap.
  - Also compared: the cursor, pending wrap, modes, Kitty flags, title,
    directory, shape, palette and colours.
  - The cases:
    - a prompt after 1,500 lines with the oldest gone;
    - vim over a shell screen, and the shell back after leaving it;
    - colours and underline styles, and the pen carrying on;
    - CJK at the right edge;
    - a scroll region with tab stops, scrolled after;
    - cursor and mouse modes;
    - a sequence cut by the end of the output;
    - autowrap turned off after a wrapped line;
    - the title with OSC 7;
    - a saved cursor restored after;
    - colours a program set;
    - synchronised output under way;
    - the alternate screen entered with 1047 and with 47.
- **`TestAResizeOverVimDrawsTheSameTerminal`** and
  **`TestTheKeptPrimaryScreenIsDroppedWhenItMayChange`** check the kept
  primary screen. The second leaves and returns within one `Write`, and
  again cut across three.
- **`TestACorePastItsMemoryBoundFails`** and
  **`TestCancellingEndsAWriteUnderWay`** check failing.
  **`TestASizePastTheLargestGridIsHeldToIt`** checks the clamp.
- **The rest:** `TestASnapshotChangesNothing`,
  `TestAtLeastScrollbackLinesAreKept`, `TestAQueryIsAnsweredOnce`,
  `TestResizeAnswersTheSizeReportAProgramAskedFor`,
  `TestASnapshotKeepsPaletteIndices`, `TestTheModuleIsThePinnedRelease` and
  `TestTheCompilerRunsTheModule` check the rest of what this page describes.
- **The page's half.** `TestTheSnapshotGolden` writes
  [`testdata/snapshot.golden`](../../internal/server/vt/testdata/snapshot.golden),
  one case to a line (`-update` rewrites it).
  - In [`wterm.test.ts`](../../web/src/terminal/emulator/wterm.test.ts), each
    case's snapshot is written into the real wterm 0.5.10. It shows the
    screen the golden names, and the same cells, history, cursor and modes
    as the shell's output written into it directly.
  - A second test there checks that wterm gives each query's reply within
    `write`, and none to `CSI 18 t`.
