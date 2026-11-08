import { useEffect, useState } from "react";
import { flushSync } from "react-dom";

/**
 * useShownOnce holds what a page shows once and the browser must not keep,
 * such as a Join Code just issued or a Secret value revealed. It lives in
 * the page's state alone, never in the URL or the browser's storage, so
 * leaving the page within the web UI loses it; and it is forgotten when the
 * browser leaves the page (pagehide), so a page the browser keeps to come
 * back to holds it no more. Setting it to undefined forgets it at once.
 */
export function useShownOnce<T>(): [T | undefined, (value: T | undefined) => void] {
  const [value, setValue] = useState<T>();
  useEffect(() => {
    const forget = () => flushSync(() => setValue(undefined));
    window.addEventListener("pagehide", forget);
    return () => window.removeEventListener("pagehide", forget);
  }, []);
  return [value, setValue];
}
