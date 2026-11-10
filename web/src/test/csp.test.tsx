import { Button } from "@heroui/react";
import { render } from "@testing-library/react";
import { expect, onTestFinished, test, vi } from "vitest";

import { takeCspViolations } from "./csp";

// The page tests' CSP watch, which setup.ts runs on every test: it records
// what the CSP, default-src 'self', would refuse, so the test fails.

test("with index.html's pressable <meta>, a HeroUI button adds no <style> element", () => {
  vi.stubEnv("NODE_ENV", "development");
  onTestFinished(() => {
    vi.unstubAllEnvs();
  });

  render(<Button>Press</Button>);

  expect(takeCspViolations()).toEqual([]);
});

test("a securitypolicyviolation the document hears is recorded", () => {
  const event = Object.assign(new Event("securitypolicyviolation"), {
    violatedDirective: "style-src-elem",
    blockedURI: "inline",
  });

  document.dispatchEvent(event);

  expect(takeCspViolations()).toEqual(["style-src-elem: inline"]);
});

test("a <style> element, an inline script and an inline handler added to the page are recorded", () => {
  const style = Object.assign(document.createElement("style"), { textContent: "* { color: red }" });
  const script = Object.assign(document.createElement("script"), { textContent: "void 0" });
  const handled = document.createElement("div");
  handled.setAttribute("onclick", "void 0");
  onTestFinished(() => {
    style.remove();
    script.remove();
    handled.remove();
  });

  document.head.append(style, script);
  document.body.append(handled);

  expect(takeCspViolations()).toEqual([
    "style-src: a <style> element: * { color: red }",
    "script-src: an inline <script>: void 0",
    "script-src-attr: an inline handler onclick on <div>",
  ]);
});

// usePress adds its <style> element as a pressable element mounts, except
// under NODE_ENV test, which Vitest sets; this test runs as a browser would.
test("without index.html's pressable <meta>, a HeroUI button adds a <style> element the watch records", () => {
  const meta = document.getElementById("react-aria-pressable-style");
  expect(meta).not.toBeNull();
  meta?.remove();
  vi.stubEnv("NODE_ENV", "development");
  onTestFinished(() => {
    vi.unstubAllEnvs();
    document.getElementById("react-aria-pressable-style")?.remove(); // the <style> it added
    document.head.append(meta!);
  });

  render(<Button>Press</Button>);

  expect(takeCspViolations()).toEqual([
    expect.stringMatching(/^style-src: a <style> element: @layer {\s*\[data-react-aria-pressable\]/),
  ]);
});
