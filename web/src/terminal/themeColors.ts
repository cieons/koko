import type { Colors } from "./connection";
import type { RGB } from "./records";

// The theme's terminal colours as the koko server answers OSC 10 and 11
// with them (spec #213, "Terminal queries"): the --term-text and
// --term-surface tokens of theme/theme.css, light or dark, as RGB.

/** byte brings a part in 0 to 1 into a byte. */
const byte = (part: number) => Math.round(Math.min(Math.max(part, 0), 1) * 255);

/** gamma is sRGB's transfer function, from a linear part. */
const gamma = (linear: number) => (linear <= 0.0031308 ? 12.92 * linear : 1.055 * linear ** (1 / 2.4) - 0.055);

/** number reads a number, a percentage as a fraction of whole. */
function number(text: string, whole: number): number {
  return text.endsWith("%") ? (Number.parseFloat(text) / 100) * whole : Number.parseFloat(text);
}

/** oklch is an OKLCH colour in sRGB (CSS Color 4's conversion). */
function oklch(l: number, c: number, hue: number): RGB {
  const h = (hue * Math.PI) / 180;
  const [a, b] = [c * Math.cos(h), c * Math.sin(h)];
  const lms = [l + 0.3963377774 * a + 0.2158037573 * b, l - 0.1055613458 * a - 0.0638541728 * b, l - 0.0894841775 * a - 1.291485548 * b].map(
    (part) => part ** 3,
  ) as [number, number, number];
  const [x, y, z] = lms;
  return [
    byte(gamma(4.0767416621 * x - 3.3077115913 * y + 0.2309699292 * z)),
    byte(gamma(-1.2684380046 * x + 2.6097574011 * y - 0.3413193965 * z)),
    byte(gamma(-0.0041960863 * x - 0.7034186147 * y + 1.707614701 * z)),
  ];
}

/** parseColor reads a colour the theme writes: #rgb, #rrggbb, rgb() or oklch(); anything else is undefined. */
export function parseColor(text: string): RGB | undefined {
  const value = text.trim().toLowerCase();
  const hex = /^#([0-9a-f]{3}|[0-9a-f]{6})$/.exec(value)?.[1];
  if (hex !== undefined) {
    const full = hex.length === 3 ? [...hex].map((digit) => digit + digit).join("") : hex;
    return [0, 2, 4].map((at) => Number.parseInt(full.slice(at, at + 2), 16)) as unknown as RGB;
  }
  const call = /^(rgba?|oklch)\(([^)]*)\)$/.exec(value);
  if (!call) {
    return undefined;
  }
  const parts = (call[2] ?? "").split(/[\s,/]+/).filter(Boolean);
  if (parts.length < 3) {
    return undefined;
  }
  const [p0 = "", p1 = "", p2 = ""] = parts;
  if (call[1] === "oklch") {
    return oklch(number(p0, 1), number(p1, 0.4), Number.parseFloat(p2));
  }
  return [p0, p1, p2].map((part) => byte(number(part, 255) / 255)) as unknown as RGB;
}

/** themeColors is the theme's terminal colours now, or undefined while the theme gives none it can read. */
export function themeColors(): Colors | undefined {
  const style = getComputedStyle(document.documentElement);
  const foreground = parseColor(style.getPropertyValue("--term-text"));
  const background = parseColor(style.getPropertyValue("--term-surface"));
  return foreground && background ? { foreground, background } : undefined;
}

/** watchTheme calls changed when the theme changes between Light and Dark, and is the function that stops it. */
export function watchTheme(changed: () => void): () => void {
  const observer = new MutationObserver(changed);
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
  return () => observer.disconnect();
}
