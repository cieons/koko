import { expect, test } from "vitest";

// Not one of core's exports: the module that holds the layer rules, by its path.
import { SLIDE_RULES_ID, slideRulesCss } from "../../node_modules/@astryxdesign/core/dist/Layer/layerSlideRules.js";
import indexHTML from "../../index.html?raw";

// Static checks of the web UI's own source, which keep it on Astryx's terms
// (ADR 0045): components from their subpaths, so the build leaves out the
// rest; only Astryx's tokens for colour, so koko's CSS is right in light
// and dark mode; and nothing loaded from another site, so the CSP stays
// default-src 'self'.

/** code is every TypeScript file of the app and its tests, by path. */
const code = import.meta.glob<string>(["../**/*.ts", "../**/*.tsx"], { query: "?raw", import: "default", eager: true });

/** css is every CSS file of koko's own, public/'s included. */
const css = import.meta.glob<string>(["../**/*.css", "../../public/*.css"], {
  query: "?raw",
  import: "default",
  eager: true,
});

/** withoutComments is CSS without its comments. */
function withoutComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "");
}

/** offending is each line of source that matches pattern, prefixed with its file. */
function offending(files: Record<string, string>, pattern: RegExp, clean: (s: string) => string = (s) => s): string[] {
  return Object.entries(files).flatMap(([path, source]) =>
    clean(source)
      .split("\n")
      .filter((line) => pattern.test(line))
      .map((line) => `${path}: ${line.trim()}`),
  );
}

// Vitest hands a test CSS it was not told to keep (vite.config.ts) as an
// empty string, which would pass every check below.
test("the checks read the app's files as written", () => {
  expect(code["../main.tsx"]).toContain("createRoot(");
  expect(css["../shell/app.css"]).toContain("@font-face");
  expect(Object.values(css).filter((source) => source.trim() === "")).toEqual([]);
  expect(indexHTML).toContain('<div id="root">');
});

test("no file imports @astryxdesign/core's root, only its components' subpaths", () => {
  expect(offending(code, /["']@astryxdesign\/core["']/)).toEqual([]);
});

// The CSS named colours, beside the hex and functional notations.
const namedColours =
  "aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood " +
  "cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray " +
  "darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen " +
  "darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue " +
  "firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green greenyellow grey honeydew " +
  "hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan " +
  "lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen lightskyblue " +
  "lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine " +
  "mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise " +
  "mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab orange " +
  "orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum " +
  "powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna " +
  "silver skyblue slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet " +
  "wheat white whitesmoke yellow yellowgreen";

/** colourLiteral matches a value that holds a colour written out rather than a token. */
const colourLiteral = new RegExp(
  String.raw`#[0-9a-f]{3,8}\b|\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix|light-dark)\(|` +
    String.raw`(^|[\s,(])(${namedColours.split(" ").join("|")})($|[\s,)])`,
  "i",
);

test("koko's CSS writes no colour literal, only Astryx's tokens", () => {
  // Every declaration, its value on one line or several; a selector, which
  // ends at a {, and a font's name are no colour.
  const literals = Object.entries(css).flatMap(([path, source]) =>
    [...withoutComments(source).matchAll(/([\w-]+)\s*:([^;{}]+)(?=[;}])/g)]
      .map(([, property = "", value = ""]) => [property, value.replace(/\s+/g, " ").trim()] as const)
      .filter(([property, value]) => !property.endsWith("font-family") && colourLiteral.test(value))
      .map(([property, value]) => `${path}: ${property}: ${value}`),
  );
  expect(literals).toEqual([]);
});

test("the theme's fonts.css, which loads Figtree from Google Fonts, is never imported", () => {
  expect(offending({ ...code, ...css }, /@astryxdesign\/[^"']*fonts\.css/)).toEqual([]);
});

test("no CSS file and not index.html names a URL of another site", () => {
  // A URL with its scheme, or one that starts // and takes the page's.
  expect(offending({ ...css, "index.html": indexHTML }, /(\bhttps?:)?\/\/[\w.-]+/i, withoutComments)).toEqual([]);
});

// Astryx's layers inject their slide rules in a <style> element, which the
// CSP refuses, unless the document holds an element of their id already.
// index.html links them under that id, from public/, which the build copies
// as it is; npm run astryx-layer-rules writes the file.
test("index.html links Astryx's layer rules as they stand in the pinned version, under the id Astryx looks for", () => {
  expect(indexHTML).toContain(`<link rel="stylesheet" id="${SLIDE_RULES_ID}" href="/astryx-layer-rules.css">`);
  const file = (css["../../public/astryx-layer-rules.css"] ?? "").replace(/^\/\*[\s\S]*?\*\/\n/, "");
  expect(file.replaceAll("\n", ""), "public/astryx-layer-rules.css is stale: run npm run astryx-layer-rules").toBe(
    slideRulesCss(),
  );
});

test("index.html runs no inline script, and carries the viewport, color-scheme and icon tags", () => {
  const scripts = [...indexHTML.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)];
  expect(scripts.length).toBeGreaterThan(0);
  for (const [script, attributes, body] of scripts) {
    expect({ script, src: /\bsrc=/.test(attributes ?? ""), body: body?.trim() }).toEqual({ script, src: true, body: "" });
  }
  expect(indexHTML.match(/<[^>]*\son[a-z]+\s*=/gi) ?? []).toEqual([]);
  // viewport-fit=cover lets Astryx's AppShell keep its top bar in a phone's safe area.
  expect(indexHTML).toContain('<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">');
  expect(indexHTML).toContain('<meta name="color-scheme" content="light dark">');
  // An icon of its own, so the browser asks for no /favicon.ico.
  expect(indexHTML).toContain('<link rel="icon" href="/favicon.svg" type="image/svg+xml">');
});
