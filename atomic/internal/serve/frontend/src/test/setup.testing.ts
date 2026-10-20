// Second preload stage — runs after setup.dom.ts has registered the DOM
// globals. Adds jest-dom's assertion matchers on top of bun:test's `expect`,
// and wires @testing-library/react's cleanup as an afterEach: its
// auto-cleanup only self-registers under jest/vitest, so bun:test needs it
// explicit or a component rendered in one test leaks into the next.
import { afterEach, expect } from "bun:test";
import * as matchers from "@testing-library/jest-dom/matchers";
import { cleanup } from "@testing-library/react";
import { __resetLoadScriptCacheForTest } from "../utils/loadScript";
import { __settleForTest as settleMemberStore } from "../utils/memberStore";
import { __resetElkForTest } from "../utils/mermaidElk";

expect.extend(matchers);

afterEach(async () => {
  cleanup();
  // Unmounting does not cancel the identity probe `useCurrentMember` starts, and
  // an unfinished one retries into the next file's fetch spy. See
  // memberStore.__settleForTest.
  await settleMemberStore();
  // loadScript's `loaded` Map, mermaidElk's pending load and railCytoscapeStyle's
  // `window.__railCy` are all module-level state shared across every test FILE in the process
  // (bun:test does not reset modules between files). Individual suites that
  // exercise the real script-load path (MiniGraph, Rail, mountMermaid, the
  // App /graph route) already reset what they touch, but relying on every
  // suite to remember is fragile — a leaked cache entry or leaked instance
  // silently short-circuits an unrelated file's test. Reset centrally too.
  __resetLoadScriptCacheForTest();
  __resetElkForTest();
  delete window.__railCy;
});
