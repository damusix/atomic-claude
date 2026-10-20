import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { __resetElkForTest, registerElkLayouts } from "./mermaidElk";

function setReadyState(value: DocumentReadyState) {
  Object.defineProperty(document, "readyState", { value, configurable: true });
}

describe("registerElkLayouts", () => {
  beforeEach(() => __resetElkForTest());

  afterEach(() => {
    __resetElkForTest();
    setReadyState("complete");
  });

  // The module's own mermaid copy has a window "load" listener that turns every
  // rendered diagram into a syntax error; it is safe only after that event.
  test("does not load the module before the window load event", async () => {
    setReadyState("loading");
    const importModule = mock(async () => ({ default: [] }));
    const done = registerElkLayouts(() => {}, importModule);
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(importModule).not.toHaveBeenCalled();

    setReadyState("complete");
    window.dispatchEvent(new Event("load"));
    await done;
    expect(importModule).toHaveBeenCalledTimes(1);
  });

  test("hands the module's default export, the layout loaders, to mermaid", async () => {
    const loaders = [{ name: "elk" }];
    const register = mock((_: unknown[]) => {});
    const importModule = mock(async (url: string) => {
      expect(url.endsWith("/vendor/mermaid-layout-elk/mermaid-layout-elk.js")).toBe(true);
      return { default: loaders };
    });
    expect(await registerElkLayouts(register, importModule)).toBe(true);
    expect(register).toHaveBeenCalledWith(loaders);
  });

  test("resolves false and registers nothing when the module cannot load, so the caller falls back to dagre", async () => {
    const register = mock(() => {});
    const importModule = async () => {
      throw new Error("404");
    };
    expect(await registerElkLayouts(register, importModule)).toBe(false);
    expect(register).not.toHaveBeenCalled();
  });

  test("loads once per page: later callers share the first attempt", async () => {
    const importModule = mock(async () => ({ default: [] }));
    const first = registerElkLayouts(() => {}, importModule);
    const second = registerElkLayouts(() => {}, importModule);
    expect(second).toBe(first);
    await first;
    expect(importModule).toHaveBeenCalledTimes(1);
  });
});
