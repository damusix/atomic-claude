import { describe, expect, test } from "bun:test";
import { __shouldWarmForTest as shouldWarm } from "./graphWarm";

const RUN = "1234-5678";

describe("graph warm lock", () => {
  test("warms when nothing has been recorded", () => {
    expect(shouldWarm(null, RUN)).toBe(true);
  });

  // A completed warm is only good for the run that produced it. A restarted
  // server may be serving different content, and its layout cache is keyed on
  // a content fingerprint the old record knows nothing about.
  test("warms again after a server restart", () => {
    expect(shouldWarm({ runId: "an-older-run", state: "done", at: Date.now() }, RUN)).toBe(true);
  });

  test("skips when this run already warmed", () => {
    expect(shouldWarm({ runId: RUN, state: "done", at: Date.now() }, RUN)).toBe(false);
  });

  // The record is a cross-tab lock: two tabs warming at once would each hold
  // a WebGL context and race to write the same cache entry.
  test("skips while another tab is building", () => {
    expect(shouldWarm({ runId: RUN, state: "building", at: Date.now() }, RUN)).toBe(false);
  });

  // A tab closed mid-warm never clears its record. Without a takeover window
  // the warm would stay blocked until the browser's storage is cleared.
  test("takes over a build abandoned long enough ago", () => {
    const abandoned = Date.now() - 10 * 60 * 1000;
    expect(shouldWarm({ runId: RUN, state: "building", at: abandoned }, RUN)).toBe(true);
  });
});
