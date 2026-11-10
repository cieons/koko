import { onTestFinished, vi } from "vitest";

/**
 * clipboard stands in for the browser's clipboard for one test, and is its
 * writeText, which records what was copied; refuse makes it refuse, as a
 * browser that does not allow copying does.
 */
export function clipboard({ refuse = false }: { refuse?: boolean } = {}) {
  const writeText = vi.fn((_: string) => (refuse ? Promise.reject(new Error("not allowed")) : Promise.resolve()));
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "clipboard");
  });
  return writeText;
}
