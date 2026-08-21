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
        setMember: (key: string) => act(() => current.setMember(key)),
      };
    },
    ...utils,
  };
}

describe("usePlansScope", () => {
  test("openSlug carries ?member= and drops ?at=", () => {
    const { router, scope } = renderHook("/plans?member=server&at=stale-branch");

    scope.openSlug("atomic-doctor");

    expect(router.state.location.pathname).toBe("/plans/atomic-doctor");
    expect(router.state.location.search).toBe("?member=server");
  });

  test("openSlug with no ?member= carries no search", () => {
    const { router, scope } = renderHook("/plans");

    scope.openSlug("atomic-doctor");

    expect(router.state.location.pathname).toBe("/plans/atomic-doctor");
    expect(router.state.location.search).toBe("");
  });

  test("openFile carries ?member= and ?at= from the current slug route", () => {
    const { router, scope } = renderHook("/plans/atomic-doctor/docs/spec/atomic-doctor.md?member=server&at=main");

    scope.openFile("findings/volatility.md");

    expect(router.state.location.pathname).toBe("/plans/atomic-doctor/findings/volatility.md");
    expect(router.state.location.search).toBe("?member=server&at=main");
  });

  test("setAt rewrites ?at= and keeps the hash", () => {
    const { router, scope } = renderHook("/plans/atomic-doctor/docs/spec/atomic-doctor.md?member=server&at=main#goal");

    scope.setAt("plans-page");

    expect(router.state.location.search).toBe("?member=server&at=plans-page");
    expect(router.state.location.hash).toBe("#goal");
  });

  test("scopedSearch is empty off-realm (no ?member=)", () => {
    const { scope } = renderHook("/plans/atomic-doctor");
    expect(scope.scopedSearch()).toBe("");
  });

  test("scopedSearch carries ?member= in realm scope", () => {
    const { scope } = renderHook("/plans/atomic-doctor?member=server");
    expect(scope.scopedSearch()).toBe("?member=server");
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

  test("setMember rewrites ?member= without touching the pathname", () => {
    const { router, scope } = renderHook("/plans");

    scope.setMember("atomic");

    expect(router.state.location.pathname).toBe("/plans");
    expect(router.state.location.search).toBe("?member=atomic");
  });

  test("setMember with an empty key drops ?member=", () => {
    const { router, scope } = renderHook("/plans?member=atomic");

    scope.setMember("");

    expect(router.state.location.search).toBe("");
  });

  test("setMember drops ?at= — another repo's branches are its own", () => {
    const { router, scope } = renderHook("/plans?member=alpha&at=feature-x");

    scope.setMember("beta");

    expect(router.state.location.search).toBe("?member=beta");
  });

  test("isPlansRoute is a segment boundary, not a prefix", () => {
    expect(renderHook("/plans").scope.isPlansRoute).toBe(true);
    expect(renderHook("/plans/x/docs/spec/x.md").scope.isPlansRoute).toBe(true);
    expect(renderHook("/plans-comparison").scope.isPlansRoute).toBe(false);
    expect(renderHook("/page/docs/x.md").scope.isPlansRoute).toBe(false);
  });
});
