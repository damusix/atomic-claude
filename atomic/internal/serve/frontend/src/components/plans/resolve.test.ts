import { describe, expect, test } from "bun:test";
import type { PlanCheckout, PlanDoc, PlanRow } from "../../utils/plansApi";
import { resolveBundleFile, resolveDocVersion } from "./resolve";

function checkout(overrides: Partial<PlanCheckout> = {}): PlanCheckout {
  return { id: "w-main", branch: "main", path: ".", outsideRoot: false, isMain: true, fileMtime: "2026-08-19T00:00:00Z", ...overrides };
}

// Merged (`main`) is older than the worktree version — the newest-by-mtime
// default must pick the worktree, not the merged version.
const DOC: PlanDoc = {
  path: "docs/spec/atomic-doctor.md",
  versions: [
    {
      sha: "sha-main",
      label: "main",
      isMain: true,
      mtime: "2026-08-18T00:00:00Z",
      checkouts: [checkout({ id: "w-main", branch: "main" })],
    },
    {
      sha: "sha-scope",
      label: "scope-marker-docs",
      isMain: false,
      mtime: "2026-08-19T00:00:00Z",
      checkouts: [checkout({ id: "w-scope", branch: "scope-marker-docs", path: ".claude/worktrees/scope-marker-docs", isMain: false })],
    },
  ],
};

describe("resolveDocVersion", () => {
  test("with at undefined, resolves to the newest-by-mtime version even when the merged version is older", () => {
    const res = resolveDocVersion(DOC, undefined);
    expect(res?.version.label).toBe("scope-marker-docs");
    expect(res?.held).toBe(false);
  });

  test("a held at naming the older merged version's checkout still resolves to it", () => {
    const res = resolveDocVersion(DOC, "main");
    expect(res?.version.label).toBe("main");
    expect(res?.held).toBe(true);
  });

  test("an at that matches no checkout of the doc yields to the newest version", () => {
    const res = resolveDocVersion(DOC, "unrelated-branch");
    expect(res?.version.label).toBe("scope-marker-docs");
    expect(res?.held).toBe(false);
  });

  test("a doc with a single version still resolves to it", () => {
    const single: PlanDoc = { path: "docs/spec/only-version.md", versions: [DOC.versions[0]] };
    const res = resolveDocVersion(single, undefined);
    expect(res?.version.label).toBe("main");
    expect(res?.held).toBe(false);
  });
});

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
      path: ".",
      outsideRoot: false,
      purposes: ["plan"],
      status: "active",
      files: [{ relpath: ".claude/.scratchpad/two-bundles/BRIEF.md", kind: "markdown" }],
    },
    {
      worktreeId: "w-feature",
      branch: "feature-x",
      path: ".claude/worktrees/feature-x",
      outsideRoot: false,
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
