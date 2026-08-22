import { describe, expect, test } from "bun:test";
import type { PlanRow } from "../../utils/plansApi";
import { resolveBundleFile } from "./resolve";

// Two bundles (branches "main" and "feature-x") both hold BRIEF.md — bundles
// never dedup across checkouts, so a reader must be able to pick either.
const ROW: PlanRow = {
  slug: "two-bundles",
  title: "two-bundles",
  description: "",
  docs: [],
  bundles: [
    {
      worktreeId: "w-main",
      branch: "main",
      purposes: ["plan"],
      status: "active",
      files: [{ relpath: ".claude/.scratchpad/two-bundles/BRIEF.md", kind: "markdown" }],
    },
    {
      worktreeId: "w-feature",
      branch: "feature-x",
      purposes: ["implement"],
      status: "active",
      files: [{ relpath: ".claude/worktrees/feature-x/.claude/.scratchpad/two-bundles/BRIEF.md", kind: "markdown" }],
    },
  ],
  dotCount: 0,
  dotMerged: false,
};

describe("resolveBundleFile", () => {
  test("with at set to a branch, returns that branch's bundle", () => {
    const res = resolveBundleFile(ROW, ".claude/worktrees/feature-x/.claude/.scratchpad/two-bundles/BRIEF.md", "feature-x");
    expect(res?.bundle.worktreeId).toBe("w-feature");
  });

  test("with at undefined, returns the first bundle holding the relpath", () => {
    const res = resolveBundleFile(ROW, ".claude/.scratchpad/two-bundles/BRIEF.md", undefined);
    expect(res?.bundle.worktreeId).toBe("w-main");
  });

  test("with at set but not matching any bundle holding the relpath, falls back to the first match", () => {
    const res = resolveBundleFile(ROW, ".claude/.scratchpad/two-bundles/BRIEF.md", "unrelated-branch");
    expect(res?.bundle.worktreeId).toBe("w-main");
  });
});
