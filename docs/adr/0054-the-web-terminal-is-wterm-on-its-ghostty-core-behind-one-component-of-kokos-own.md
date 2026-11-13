---
status: its core as the one emulator of a terminal, refined by ADR-0058
---

# The web terminal is wterm on its ghostty core, behind one component of koko's own

The web terminal (ADR 0053) is rendered by wterm (Apache-2.0) with its
libghostty core, the Owner's choice of 2026-11-12: `@wterm/react`,
`@wterm/dom`, `@wterm/core` and `@wterm/ghostty`, pinned to exactly 0.5.10.
wterm renders each row as DOM text, so selection, the soft keyboard, input
methods such as Chinese pinyin, paste and screen readers are the browser's
own. That is what a terminal on a phone needs most. Its ghostty core is
libghostty's VT core built from Ghostty v1.3.1's source with patches for
WebAssembly. It is not the separately released libghostty-vt 1.0, so koko's
core moves when wterm ships a rebuilt binary.

Only one directory of koko's own, `web/src/terminal/emulator/`, imports
wterm, and a static check fails any other file that does. The page talks to it
through a few calls: write output, input, resize, focus, and a key press for
the phone's key bar. Falling back to wterm's own core, or to xterm.js,
changes that directory alone. The core's WebAssembly, 197 KB gzipped, is
served as a hashed file of the build with `application/wasm`. It loads with
the terminal page's chunk, so the first load does not change, and like every
hashed file it is cached for a year.

The CSP gains one directive, `script-src 'self' 'wasm-unsafe-eval'`, in ADR
0046's terms. It allows compiling WebAssembly from the build's own file and
nothing else: no inline script and no `eval`. wterm adds no `<style>`
element, and its code uses no `data:` or `blob:` URL and starts no worker.
This refines ADR 0046.

wterm 0.5.10's renderer, as published, writes each row as HTML whose runs
of cells carry `style` attributes, which a CSP without `'unsafe-inline'`
for styles refuses: the design preview drew the terminal garbled under
kokod's policy (#169). koko patches the renderer (`patch-package`, applied
at install from `web/patches/`): the rows carry their styles in
`data-term-style`, set through CSSOM right after the parser, which
`style-src` does not govern. So kokod's CSP is unchanged, and `style-src`
keeps its one hash. The change is to be sent to wterm, and the patch is
dropped once wterm ships it; a wterm upgrade makes it again, since the
install fails when it no longer applies. `style-src-attr 'unsafe-inline'`
was the fallback the Owner did not take (2026-11-12).

## Considered Options

- **xterm.js 6:** the emulator nearly every product ships, with the deepest
  VT coverage. But it writes `<style>` elements with computed text, so it
  needs `style-src 'unsafe-inline'`, giving up ADR 0046's rule. Its
  maintainer calls phone input out of scope, touch scrolling is broken in
  its stable release, and Chinese input bugs are open on iOS.
- **xterm.js 5.5 pinned:** reported to run under a strict policy with
  console errors, which koko's page tests fail on, and a release left
  behind.
- **ghostty-web:** libghostty in a canvas, but stalled since mid-2026, with
  no screen reader support, and it loads its WebAssembly from a `data:` URL
  that the CSP blocks.
- **restty:** WebGPU, with one maintainer, about 800 KB, and early.
- **hterm:** maintained in ChromeOS's source but not installable from npm,
  with no composition handling found.
- **wterm's own core first:** smaller, and inlined, but with fewer VT
  features than libghostty's (graphemes, underline styles, row-wrap
  metadata). It stays the first fallback.

## Consequences

- wterm is seven months old, labelled an experiment, and on 0.x releases.
  koko pins exact versions. An upgrade is a commit of its own that makes the
  renderer's patch again (or drops it) and runs the static checks, the size
  budget and Vitest, whose renderer test fails on a style attribute given
  to the HTML parser; the Owner's check on a phone (vim, tmux, htop and
  Chinese input) follows before a release.
- What wterm documents about phones and input methods was read from its
  README and source, not seen on a device. A throwaway preview under
  kokod's exact CSP settles it before the page is built.
- The build on disk grows by about 1.1 MB, against the 3 MiB budget. A
  build that would pass it stops for the Owner's yes before the budget is
  raised.
- Falling back to xterm.js would need `style-src 'unsafe-inline'`, and so a
  new ADR.
