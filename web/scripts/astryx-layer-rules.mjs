// Writes public/astryx-layer-rules.css: the rules Astryx's layers (Dialog,
// Popover, Menu, Tooltip) would inject in a <style> element, which the CSP,
// default-src 'self', refuses. index.html links the file under the element
// id Astryx looks for, so it injects nothing; Vite copies public/ as it is,
// keeping the link and its id. Run it, through npm run astryx-layer-rules,
// after every Astryx upgrade; src/test/static.test.ts fails until it has.
import { writeFileSync } from "node:fs";

// Not one of core's exports: the module that holds the rules, by its path.
const { slideRulesCss } = await import("../node_modules/@astryxdesign/core/dist/Layer/layerSlideRules.js");

const header = `/*
 * Astryx's layer slide rules, which its layers would inject in a <style>
 * element that the CSP refuses. Written by npm run astryx-layer-rules from
 * the pinned @astryxdesign/core; never edited by hand.
 */
`;

// One rule to a line, so an upgrade's diff shows what changed.
writeFileSync(
  new URL("../public/astryx-layer-rules.css", import.meta.url),
  header + slideRulesCss().replace(/\}(?=[@.[:])/g, "}\n") + "\n",
);
