// The page tests' CSP watch. The web UI runs under default-src 'self'
// (docs/subsystems/web-front.md), which refuses a <style> element, an
// inline script and an inline event handler, among others, and reports each
// refusal as a securitypolicyviolation event. jsdom enforces no CSP, so the
// watch records both: every such event the document hears, and every
// element the CSP would refuse as it is added, which a real browser would
// report as one. setup.ts fails a test that leaves any.

const violations: string[] = [];
let observer: MutationObserver | undefined;

/** refused is why the CSP would refuse element, or undefined when it would not. */
function refused(element: Element): string | undefined {
  if (element instanceof HTMLStyleElement) {
    return `style-src: a <style> element: ${element.textContent?.trim()}`;
  }
  if (element instanceof HTMLScriptElement && !element.src) {
    return `script-src: an inline <script>: ${element.textContent?.trim()}`;
  }
  const handler = [...element.attributes].find((a) => /^on[a-z]+$/i.test(a.name));
  if (handler) {
    return `script-src-attr: an inline handler ${handler.name} on <${element.localName}>`;
  }
  return undefined;
}

/** installCspWatch starts recording violations on the document, for the whole test run. */
export function installCspWatch(): void {
  document.addEventListener("securitypolicyviolation", (event) => {
    const { violatedDirective, blockedURI } = event as SecurityPolicyViolationEvent;
    violations.push(`${violatedDirective}: ${blockedURI}`);
  });
  observer = new MutationObserver(record);
  observer.observe(document, { childList: true, subtree: true });
}

/** record records each element added that the CSP would refuse. */
function record(records: MutationRecord[]): void {
  for (const { addedNodes } of records) {
    for (const node of addedNodes) {
      if (node instanceof Element) {
        for (const element of [node, ...node.querySelectorAll("*")]) {
          const why = refused(element);
          if (why !== undefined) {
            violations.push(why);
          }
        }
      }
    }
  }
}

/** takeCspViolations is every violation recorded since it was last taken, which it forgets. */
export function takeCspViolations(): string[] {
  record(observer?.takeRecords() ?? []);
  return violations.splice(0);
}
