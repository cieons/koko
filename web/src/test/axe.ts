import axe from "axe-core";

// The accessibility check every page test runs on each page and dialog
// state it renders: axe-core's rules for WCAG 2.2 at levels A and AA, all
// but color-contrast, which jsdom cannot compute since it has no layout and
// no colours. The real-browser check runs that one (docs/subsystems/web-ui.md).

/** wcag22AA are axe's tags for every WCAG 2.0, 2.1 and 2.2 rule at levels A and AA. */
const wcag22AA = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"];

/**
 * expectAccessible fails the test with each violation of WCAG 2.2 A and AA
 * axe finds in what is rendered (the whole document, by default), naming
 * the rule and the elements at fault.
 */
export async function expectAccessible(context: Element | Document = document): Promise<void> {
  const results = await axe.run(context, {
    runOnly: { type: "tag", values: wcag22AA },
    rules: { "color-contrast": { enabled: false } },
  });
  if (results.violations.length > 0) {
    const found = results.violations.map(
      (v) => `${v.id}: ${v.help}\n${v.nodes.map((n) => `    ${n.html}`).join("\n")}`,
    );
    throw new Error(`axe found ${found.length} violations of WCAG 2.2 A and AA:\n  ${found.join("\n  ")}`);
  }
}
