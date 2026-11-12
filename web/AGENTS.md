# AGENTS

Project-specific guidance for AI coding agents.

koko's web UI, on HeroUI in koko's Obsidian theme (ADR 0046): read
[the web UI's design](../docs/web-design.md), which says what every screen
shows and the values it uses, [the web UI build](../docs/subsystems/web-ui.md)
and [the web UI shell](../docs/subsystems/web-shell.md) first, and the
repository's own [AGENTS.md](../AGENTS.md).

- Look HeroUI's components up in its documentation,
  `https://heroui.com/react/llms.txt`, and check what it says against the
  pinned package's types in `node_modules/@heroui/react/dist`, rather than
  from memory. The version is the one `package.json` pins.
- Build on the shell's parts before HeroUI's own: the frame, the page head
  and panel, the states every screen has, the dialogs, the fields (a
  searchable `SearchChoice` wherever a person picks one item from a list
  that can grow) and the rest in `src/ui/` and `src/shell/`.
- Layout is Tailwind's utilities; a colour is written in
  `src/theme/theme.css` alone, as a token, and everything else uses the
  tokens. A HeroUI component taken up for the first time adds its style
  file to `src/theme/heroui.css`, in HeroUI's order.
- The CSP is `default-src 'self'`, with one hash in `style-src` for the
  `<style>` element React Aria adds on iOS Safari and `'wasm-unsafe-eval'`
  in `script-src` for the web terminal's emulator: no inline script, no
  other `<style>` element, no `data:` URL and no file from another site. Every string comes from the two catalogues, in the formal register
  of product UI.
