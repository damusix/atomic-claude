import { describe, expect, test } from "bun:test";
import { act, render } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { usePlansScope, type PlansScope } from "./usePlansScope";

function renderHook(initialPath: string) {
  let captured: PlansScope | undefined;
  function Probe() {
    captured = usePlansScope();
    return null;
  }
  const router = createMemoryRouter([{ path: "*", element: <Probe /> }], { initialEntries: [initialPath] });
  const utils = render(<RouterProvider router={router} />);
  return {
    router,
    get scope() {
      const current = captured!;
      return {
        ...current,
        openSlug: (slug: string) => act(() => current.openSlug(slug)),
        openFile: (relpath: string, opts?: { replace?: boolean }) => act(() => current.openFile(relpath, opts)),
        setAt: (branch: string, opts?: { replace?: boolean }) => act(() => current.setAt(branch, opts)),
      };
    },
    ...utils,
  };
}

describe("usePlansScope", () => {
  test("plansHref is always /plans", () => {
    expect(renderHook("/plans").scope.plansHref()).toBe("/plans");
    expect(renderHook("/plans?at=stale-branch").scope.plansHref()).toBe("/plans");
  });

  test("slugHref carries only ?at=", () => {
    expect(renderHook("/plans").scope.slugHref("atomic-doctor")).toBe("/plans/atomic-doctor");
    expect(renderHook("/plans?at=main").scope.slugHref("atomic-doctor")).toBe("/plans/atomic-doctor?at=main");
  });

  test("scopedSearch no longer exists", () => {
    const { scope } = renderHook("/plans");
    expect((scope as unknown as Record<string, unknown>).scopedSearch).toBeUndefined();
  });

  test("openSlug navigates to /plans/<slug> with no search", () => {
    const { router, scope } = renderHook("/plans?at=stale-branch");

    scope.openSlug("atomic-doctor");

    expect(router.state.location.pathname).toBe("/plans/atomic-doctor");
    expect(router.state.location.search).toBe("");
  });

  test("openFile carries ?at= from the current slug route", () => {
    const { router, scope } = renderHook("/plans/atomic-doctor/docs/spec/atomic-doctor.md?at=main");

    scope.openFile("findings/volatility.md");

    expect(router.state.location.pathname).toBe("/plans/atomic-doctor/findings/volatility.md");
    expect(router.state.location.search).toBe("?at=main");
  });

  test("setAt rewrites ?at= and keeps the hash", () => {
    const { router, scope } = renderHook("/plans/atomic-doctor/docs/spec/atomic-doctor.md?at=main#goal");

    scope.setAt("plans-page");

    expect(router.state.location.search).toBe("?at=plans-page");
    expect(router.state.location.hash).toBe("#goal");
  });

  test("slug and relpath parse from the /plans/:slug/* route", () => {
    const { scope } = renderHook("/plans/atomic-doctor/docs/spec/atomic-doctor.md");
    expect(scope.slug).toBe("atomic-doctor");
    expect(scope.relpath).toBe("docs/spec/atomic-doctor.md");
  });

  test("slug and relpath are undefined on the bare /plans route", () => {
    const { scope } = renderHook("/plans");
    expect(scope.slug).toBeUndefined();
    expect(scope.relpath).toBeUndefined();
  });

  test("isPlansRoute is a segment boundary, not a prefix", () => {
    expect(renderHook("/plans").scope.isPlansRoute).toBe(true);
    expect(renderHook("/plans/x/docs/spec/x.md").scope.isPlansRoute).toBe(true);
    expect(renderHook("/plans-comparison").scope.isPlansRoute).toBe(false);
    expect(renderHook("/page/docs/x.md").scope.isPlansRoute).toBe(false);
  });
});
