import { afterEach, describe, expect, test } from "bun:test";
import { __resetLoadScriptCacheForTest, loadScript } from "./loadScript";

// happy-dom's HTMLScriptElement throws synchronously on connect when actual
// script-file loading is requested (JS file loading is disabled by this
// project's test config, correctly — a unit test has no business fetching a
// real vendor bundle over the network). Substituting a <div> for the
// "script" tag keeps loadScript's own DOM plumbing (createElement/src/
// addEventListener/appendChild) under real test coverage while sidestepping
// happy-dom's script-specific network guard entirely.
function stubScriptLoad(shouldFail = false) {
  const origCreate = document.createElement.bind(document);
  let createCount = 0;
  document.createElement = ((tag: string) => {
    if (tag === "script") {
      createCount++;
      const el = origCreate("div") as unknown as HTMLScriptElement;
      queueMicrotask(() => el.dispatchEvent(new Event(shouldFail ? "error" : "load")));
      return el;
    }
    return origCreate(tag);
  }) as typeof document.createElement;
  return {
    restore: () => {
      document.createElement = origCreate;
    },
    createCount: () => createCount,
  };
}

describe("loadScript", () => {
  afterEach(() => {
    __resetLoadScriptCacheForTest();
    document.head.innerHTML = "";
  });

  test("appends one script element and resolves on load", async () => {
    const stub = stubScriptLoad();
    await loadScript("/vendor/mermaid.min.js");
    expect(stub.createCount()).toBe(1);
    stub.restore();
  });

  test("a second call for the same src reuses the cached load — no second element created", async () => {
    const stub = stubScriptLoad();
    await loadScript("/vendor/mermaid.min.js");
    await loadScript("/vendor/mermaid.min.js");
    expect(stub.createCount()).toBe(1);
    stub.restore();
  });

  test("rejects when the script fails to load", async () => {
    const stub = stubScriptLoad(true);
    await expect(loadScript("/vendor/broken.js")).rejects.toThrow();
    stub.restore();
  });
});
