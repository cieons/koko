# The web UI is built on Astryx

The web UI is built on Astryx, Meta's open source design system (MIT; React
19 and StyleX, used through its prebuilt CSS and typed React components), in
its Neutral theme, the Owner's choice of 2026-11-09. Each page adopts
Astryx's page patterns, such as the app shell with side navigation, the
filterable table, the detail page and the dialog flow, with layout from
Astryx's components and values from its design tokens. It is not a re-skin
of koko's own layouts. The web UI follows the system's light or dark mode,
and a Light / Dark / System switch overrides it. Every string stays in koko's
two catalogues, written in the formal register of product UI. This refines
ADR 0023: the web UI is still React and TypeScript, built by make and never
committed.

## Considered Options

- **Plain CSS, as before**: no dependency, but every page hand-styled, no
  dark mode, and phones handled page by page.
- **Radix or React Aria primitives, styled by koko**: accessible behaviour,
  but the look and its consistency would still be koko's to design and
  keep.
- **MUI or Mantine**: mature, but each brings a styling runtime of its own
  and a heavier bundle.
- **Astryx built from its StyleX source** (`@astryxdesign/build`): smaller
  CSS, but a Babel plugin in koko's build.
- **Tailwind beside Astryx**: a second styling vocabulary for what Astryx's
  tokens and a little plain CSS already cover.

## Consequences

- Astryx is Beta: 0.x, and a minor release may break things. Its packages
  are pinned to exact versions, and an upgrade is a commit of its own that
  runs the page tests, the size budget and a real-browser check. CI runs
  `npm audit signatures` against their provenance attestations. Their
  `postinstall` scripts stay off through `allowScripts`.
- `kokod` embeds a larger web UI, so the build has a budget: at most 3 MB on
  disk, and at most 450 KB gzipped for the first load. Components are
  imported from their own subpaths so the build leaves out the rest.
- The theme's fonts would load from Google Fonts. koko bundles Figtree
  instead, so the web UI loads nothing from another site and its CSP stays
  `default-src 'self'`.
- Astryx's own strings, such as a dialog's close label, come from its
  Simplified Chinese catalogue. Dates still show as koko formats them.
- Agents working in `web/` use Astryx's CLI at the pinned version, and its
  managed agent-docs block is committed in `web/AGENTS.md` and
  `web/CLAUDE.md`. The repository's own `AGENTS.md` and `CLAUDE.md` stay
  koko's.
- Page tests assert behaviour and accessibility (axe), not layout, so a page
  may change its layout while every action it offered stays possible.
