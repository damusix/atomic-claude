import { afterEach, describe, expect, test } from "bun:test";
import { renderHook, act } from "@testing-library/react";
import { __resetForTest, clearOnScreen, getOnScreen, setOnScreen, useOnScreenCheckout } from "./planViewStore";

describe("planViewStore", () => {
  afterEach(() => {
    __resetForTest();
  });

  test("setOnScreen then read returns the published checkout", () => {
    setOnScreen({ branch: "main", path: "api", outsideRoot: false });

    expect(getOnScreen()).toEqual({ branch: "main", path: "api", outsideRoot: false });
  });

  test("clearOnScreen returns null", () => {
    setOnScreen({ branch: "main", path: "api", outsideRoot: false });

    clearOnScreen();

    expect(getOnScreen()).toBeNull();
  });

  test("useOnScreenCheckout re-renders a subscriber on set", () => {
    const { result } = renderHook(() => useOnScreenCheckout());
    expect(result.current).toBeNull();

    act(() => {
      setOnScreen({ branch: "worktree-billing", path: "api/.claude/worktrees/billing", outsideRoot: false });
    });

    expect(result.current).toEqual({
      branch: "worktree-billing",
      path: "api/.claude/worktrees/billing",
      outsideRoot: false,
    });
  });
});
