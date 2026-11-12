import { expect, test } from "vitest";

import webGo from "../../../internal/server/web/web.go?raw";
import herouiIndex from "../../node_modules/@heroui/styles/dist/components/index.css?raw";
import usePress from "../../node_modules/react-aria/dist/private/interactions/usePress.mjs?raw";
import usePreventScroll from "../../node_modules/react-aria/dist/private/overlays/usePreventScroll.mjs?raw";
import indexHTML from "../../index.html?raw";
import packageLock from "../../package-lock.json?raw";
import packageJSON from "../../package.json?raw";

// Static checks of the web UI's own source, which keep it on ADR 0046's
// terms: a colour is written in the theme's stylesheet alone, so the rest is
// right in light and dark mode; HeroUI's component styles come in the order
// HeroUI gives them; and the CSP stays met: nothing loaded from another
// site, no inline script or handler, and each <style> element the pinned
// React Aria adds either kept out by index.html's <meta> or allowed by its
// hash in kokod's header.

/** code is every TypeScript file of the app and its tests, by path. */
const code = import.meta.glob<string>(["../**/*.ts", "../**/*.tsx"], { query: "?raw", import: "default", eager: true });

/** patches are the changes patch-package makes to installed packages, by path. */
const patches = import.meta.glob<string>("../../patches/*.patch", { query: "?raw", import: "default", eager: true });

/** css is every CSS file of koko's own, by path. */
const css = import.meta.glob<string>(["../**/*.css"], {
  query: "?raw",
  import: "default",
  eager: true,
});

/** themePath is the one stylesheet that writes a colour. */
const themePath = "../theme/theme.css";

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
  expect(css[themePath]).toContain("@font-face");
  expect(Object.values(css).filter((source) => source.trim() === "")).toEqual([]);
  expect(indexHTML).toContain('<div id="root">');
  expect(webGo).toContain("func securityHeaders(");
  expect(usePress).toContain("STYLE_ID");
  expect(usePreventScroll).toContain("createElement('style')");
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
    String.raw`(^|[\s,(_])(${namedColours.split(" ").join("|")}|transparent|currentcolor)($|[\s,)_])`,
  "i",
);

test("a colour is written in the theme's stylesheet alone: no other CSS file writes one", () => {
  // Every declaration, its value on one line or several; a selector, which
  // ends at a {, and a font's name are no colour.
  const literals = Object.entries(css)
    .filter(([path]) => path !== themePath)
    .flatMap(([path, source]) =>
      [...withoutComments(source).matchAll(/([\w-]+)\s*:([^;{}]+)(?=[;}])/g)]
        .map(([, property = "", value = ""]) => [property, value.replace(/\s+/g, " ").trim()] as const)
        .filter(([property, value]) => !property.endsWith("font-family") && colourLiteral.test(value))
        .map(([property, value]) => `${path}: ${property}: ${value}`),
    );
  expect(literals).toEqual([]);
});

test("a colour is written in the theme's stylesheet alone: no class name writes one in an arbitrary value", () => {
  // Tailwind's arbitrary values, such as shadow-[0_0_0_3px_var(--ok-halo)],
  // in the app's code; its tests aside.
  const app = Object.fromEntries(Object.entries(code).filter(([path]) => !/\.test\.tsx?$|\/test\//.test(path)));
  const literals = Object.entries(app).flatMap(([path, source]) =>
    [...source.matchAll(/[\w-]+-\[([^\]\s"'`]+)\]/g)]
      .filter(([, value = ""]) => colourLiteral.test(value))
      .map(([written]) => `${path}: ${written}`),
  );
  expect(literals).toEqual([]);
});

test("HeroUI's component styles are imported in the order HeroUI's own index gives them", () => {
  const order = [...herouiIndex.matchAll(/@import "\.\/([\w-]+)\.css"/g)].map(([, name]) => name);
  const imported = [...(css["../theme/heroui.css"] ?? "").matchAll(/@heroui\/styles\/components\/([\w-]+)\.css/g)].map(
    ([, name]) => name,
  );
  expect(imported.length).toBeGreaterThan(0);
  expect(imported.filter((name) => !order.includes(name))).toEqual([]);
  expect(imported).toEqual([...imported].sort((a, b) => order.indexOf(a) - order.indexOf(b)));
});

test("no CSS file and not index.html names a URL of another site", () => {
  // A URL with its scheme, or one that starts // and takes the page's.
  expect(offending({ ...css, "index.html": indexHTML }, /(\bhttps?:)?\/\/[\w.-]+/i, withoutComments)).toEqual([]);
});

// React Aria's usePress adds its rule for pressable elements in a <style>
// element, which the CSP refuses, unless the document holds an element of
// the id it looks for. index.html holds a <meta> of that id, and the theme
// holds the rule itself.
test("index.html holds the <meta> whose id the pinned React Aria looks for, and the theme its rule", () => {
  const id = /STYLE_ID = '([\w-]+)'/.exec(usePress)?.[1];
  expect(id).toBe("react-aria-pressable-style");
  expect(indexHTML).toContain(`<meta id="${id}" name="${id}" content="static">`);
  const attribute = /PRESSABLE_ATTRIBUTE = '([\w-]+)'/.exec(usePress)?.[1];
  const declaration = /style\.textContent = `[^`]*\{\s*([\w-]+:[^;}]+);\s*\}\s*\}\s*`/.exec(usePress)?.[1];
  expect([attribute, declaration]).toEqual(["data-react-aria-pressable", "touch-action: pan-x pan-y pinch-zoom"]);
  expect(withoutComments(css[themePath] ?? "")).toContain(`[${attribute}] {\n    ${declaration};\n  }`);
});

/** sha256 is the base64 SHA-256 of text, as a CSP hash source writes it. */
async function sha256(text: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return btoa(String.fromCharCode(...new Uint8Array(digest)));
}

// On iOS Safari, React Aria's usePreventScroll adds a <style> element each
// time an overlay opens, checking no id first. kokod's CSP allows it by its
// hash (ADR 0046), which holds only while the pinned React Aria adds the
// same text.
test("kokod's CSP allows by its hash the <style> element the pinned React Aria adds on iOS Safari, and no other", async () => {
  const added = [...usePreventScroll.matchAll(/style\.textContent = `([\s\S]*?)`\.trim\(\)/g)].map(([, text]) =>
    (text ?? "").trim(),
  );
  expect(added).toEqual(["@layer {\n  * {\n    overscroll-behavior: contain;\n  }\n}"]);
  const hashes = [...webGo.matchAll(/'sha256-([\w+/=]+)'/g)].map(([, hash]) => hash);
  expect(hashes).toEqual([await sha256(added[0] ?? "")]);
});

test("index.html runs no inline script, and carries the viewport, color-scheme and icon tags", () => {
  const scripts = [...indexHTML.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)];
  expect(scripts.length).toBeGreaterThan(0);
  for (const [script, attributes, body] of scripts) {
    expect({ script, src: /\bsrc=/.test(attributes ?? ""), body: body?.trim() }).toEqual({ script, src: true, body: "" });
  }
  expect(indexHTML.match(/<[^>]*\son[a-z]+\s*=/gi) ?? []).toEqual([]);
  // viewport-fit=cover lets the phone's bar keep clear of the safe area.
  expect(indexHTML).toContain('<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">');
  expect(indexHTML).toContain('<meta name="color-scheme" content="light dark">');
  // An icon of its own, so the browser asks for no /favicon.ico.
  expect(indexHTML).toContain('<link rel="icon" href="/favicon.svg" type="image/svg+xml">');
});

// The web terminal's emulator is wterm (ADR 0054), kept behind one component
// so that replacing it, or its core, changes one directory.
const emulatorDir = "../terminal/emulator/";

test("only the terminal's emulator directory imports wterm", () => {
  expect(Object.keys(code).filter((path) => path.startsWith(emulatorDir)).length).toBeGreaterThan(0);
  const outside = Object.fromEntries(Object.entries(code).filter(([path]) => !path.startsWith(emulatorDir)));
  expect(offending(outside, /(\bfrom\s*|\bimport\s*\(?\s*)["']@wterm\//)).toEqual([]);
});

test("the four wterm packages are pinned to one exact version, and its renderer's patch is applied at install", () => {
  const { dependencies, scripts } = JSON.parse(packageJSON) as {
    dependencies: Record<string, string>;
    scripts: Record<string, string>;
  };
  const lock = JSON.parse(packageLock) as { packages: Record<string, { version: string }> };
  const wterm = Object.entries(dependencies).filter(([name]) => name.startsWith("@wterm/"));
  expect(wterm.map(([name]) => name).sort()).toEqual(["@wterm/core", "@wterm/dom", "@wterm/ghostty", "@wterm/react"]);
  const versions = new Set(wterm.map(([, version]) => version));
  expect(versions.size).toBe(1);
  const [version = ""] = versions;
  expect(version).toMatch(/^\d+\.\d+\.\d+$/);
  for (const [name] of wterm) {
    expect({ name, locked: lock.packages[`node_modules/${name}`]?.version }).toEqual({ name, locked: version });
  }
  // patch-package fails the install when the patch no longer applies, so
  // an upgrade cannot drop it silently: it makes the patch again, or drops
  // it once wterm sets its rows' styles through CSSOM itself.
  expect(scripts.postinstall).toBe("patch-package --error-on-fail");
  expect(Object.keys(patches)).toEqual([`../../patches/@wterm+dom+${version}.patch`]);
});
