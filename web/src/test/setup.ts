import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Each test starts from an empty document.
afterEach(() => {
  cleanup();
});
