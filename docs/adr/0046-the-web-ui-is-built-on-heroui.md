# The web UI is built on HeroUI

The web UI is built on HeroUI v3 (Apache-2.0), which is React Aria
Components styled with Tailwind CSS 4, in a theme of koko's own called
Obsidian, the Owner's choice of 2026-11-10. HeroUI gives the controls and
their behaviour; the theme, the page layouts and the interaction are koko's,
as [the web UI's design](../web-design.md) records them. The page is one
neutral colour, content lies on panels raised a little above it, one blue
marks the action to take, and the web UI follows the system's light or dark
mode unless Light, Dark or System in the top bar overrides it. Every string
stays in koko's two catalogues, written in the formal register of product
UI. This supersedes ADR 0045, which built the web UI on Astryx and rejected
Tailwind beside it, and still refines ADR 0023: the web UI is React and
TypeScript, built by make and never committed.

## Considered Options

- **Astryx, as ADR 0045 chose**: five design rounds on its Neutral theme and
  its page templates did not reach a look the Owner accepted. Its patterns
  decided the layouts, and koko has too few functions to fill them.
- **React Aria Components styled by koko alone**: the same behaviour with
  no HeroUI between, but every control's states (hover, focus, pressed,
  disabled, invalid, the overlays' motion) would be koko's to style and
  keep.
- **Keeping Astryx's pages until each is rebuilt**: every page stays
  reachable during the move, but two design systems would share one
  document, with two resets, two sets of tokens and a first load that
  carries both. The move removes Astryx and its pages at once and builds
  the pages again one at a time; v0.3.0 is held until the last is back.

## Consequences

- Astryx leaves the repository with its packages, its CLI and its managed
  block in `web/AGENTS.md` and `web/CLAUDE.md`. Tailwind CSS 4 becomes the
  web UI's one styling vocabulary: HeroUI's parts are themed through its CSS
  variables, and layout is Tailwind's utilities. koko's colours are written
  once, as the theme's tokens in one stylesheet, and every other rule and
  component uses the tokens.
- The packages are pinned to exact versions: `@heroui/react` and
  `@heroui/styles` 3.2.7, `react-aria-components` 1.22.1, `react-aria`
  3.53.1, `@internationalized/date` 3.12.4 for the date fields' values,
  `tailwindcss` and `@tailwindcss/vite` 4.3.5, React 19. HeroUI v3 needs no
  animation library: its motion is CSS. An upgrade is a commit of its own
  that runs the page tests, the size budget and a real-browser check. CI
  still runs `npm audit signatures`, and `postinstall` scripts stay off
  through `allowScripts`.
- The CSP stays `default-src 'self'`, and two things React Aria does are met
  without loosening it:
  - `usePress` adds a `<style>` for `[data-react-aria-pressable]` when it
    first runs, unless the document already holds an element whose id is
    `react-aria-pressable-style`. `index.html` holds a `<meta>` with that
    id, and koko's stylesheet holds the rule itself
    (`touch-action: pan-x pan-y pinch-zoom`). On desktop browsers the
    approved preview ran every page, panel and dialog with no CSP
    violation.
  - On iOS Safari, `usePreventScroll` adds a second `<style>`
    (`* { overscroll-behavior: contain }`) each time an overlay opens, and
    checks no id first. Under this CSP the browser refuses it and reports a
    violation, and that rule does not apply. This was read from React
    Aria's source and has not been run on an iOS device, so what a person
    sees there is not yet known. The first ticket of the move runs it on
    one and then takes one of two paths, recorded here: a patch of the
    pinned `react-aria` that drops the element, with the rule in koko's
    stylesheet under a class the overlay sets; or the element's hash in the
    CSP's `style-src`, which is a change to the header kokod sends.
  - A page test fails on any `securitypolicyviolation` event, so an upgrade
    that adds a third case is caught.
- React Aria ships its own strings in some thirty languages. The build
  keeps English and Simplified Chinese through
  `@react-aria/optimize-locales-plugin`, and HeroUI's parts take the
  language from one `I18nProvider`.
- HeroUI's stylesheet holds every component. koko imports only the
  components it uses, one file each, in the order HeroUI's own index gives
  them.
- The fonts are Geist and Geist Mono (SIL Open Font License 1.1), their
  Latin subsets bundled as two variable WOFF2 files, 29 kB and 23 kB, with
  both licence files beside them. Chinese text uses the system's fonts.
  Nothing loads from another site.
- The budget stays as ADR 0045 set it: at most 3 MB on disk and at most
  450 KB gzipped for the first load. The preview's first load is 287 kB:
  202 kB of script, 32 kB of styles and 52 kB of fonts, with 1.2 MB on
  disk. Every page past the first is a chunk of its own, and the calendar,
  which is 38 kB gzipped, loads with the form that uses it.
- The Owner chose the lighter edge on text fields, selects and checkboxes
  (2026-11-10), knowing that it is about 1.5:1 against a panel and so under
  the 3:1 of WCAG 2.2's criterion 1.4.11. The fields still show their
  label, their focus ring and their invalid state at full contrast. axe
  does not test that criterion, so the checks stay clean; the design
  records the choice so that a later review does not undo it by mistake.
- Page tests assert behaviour and accessibility (axe, all rules but colour
  contrast, which jsdom cannot compute), not layout, as before; the
  real-browser check runs axe with colour contrast, light and dark. Every
  action in the page inventory stays possible by the same call, and every
  URL a released koko served redirects to its new place, keeping its
  query.
- Agents working in `web/` read [the web UI's design](../web-design.md)
  and HeroUI's own documentation (`https://heroui.com/react/llms.txt`),
  checked against the pinned package's types, rather than a CLI.
