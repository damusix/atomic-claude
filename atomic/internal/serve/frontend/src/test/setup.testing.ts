// Second preload stage — runs after setup.dom.ts has registered the DOM
// globals. Adds jest-dom's assertion matchers on top of bun:test's `expect`,
// and wires @testing-library/react's cleanup as an afterEach: its
// auto-cleanup only self-registers under jest/vitest, so bun:test needs it
// explicit or a component rendered in one test leaks into the next.
import { afterEach, expect } from "bun:test";
import * as matchers from "@testing-library/jest-dom/matchers";
import { cleanup } from "@testing-library/react";

expect.extend(matchers);

afterEach(() => {
  cleanup();
});
