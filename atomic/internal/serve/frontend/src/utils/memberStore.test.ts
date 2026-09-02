import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import {
  __resetForTest,
  __settleForTest,
  ensureIdentity,
  getState,
  memberLabel,
  setMember,
} from "./memberStore";

function clearCookie() {
  document.cookie = "atomic-member=; path=/; max-age=0";
}

function mockNav(body: unknown, ok = true) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), { status: ok ? 200 : 500, headers: { "Content-Type": "application/json" } }),
  ) as unknown as typeof fetch;
}

describe("memberStore", () => {
  // The store is a module-level singleton shared by every test file bun
  // runs in this process — other suites' components call ensureIdentity()
  // too, so state must be reset going in, not just coming out.
  beforeEach(() => {
    __resetForTest();
    clearCookie();
  });

  afterEach(() => {
    mock.restore();
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
    __resetForTest();
    clearCookie();
  });

  test("settling a failing probe drains its retries, so none can land on the next file's spy", async () => {
    let calls = 0;
    globalThis.fetch = mock(async () => {
      calls += 1;
      throw new Error("fetch torn down by the suite that started this probe");
    }) as unknown as typeof fetch;

    // Fire-and-forget, exactly as useCurrentMember's effect does it.
    void ensureIdentity();
    await __settleForTest();

    const afterSettle = calls;
    // A retry surviving the settle would land in this window, which stands in
    // for the next test file's spy.
    await new Promise((resolve) => setTimeout(resolve, 600));

    expect(calls).toBe(afterSettle);
    expect(getState().ready).toBe(true);
  });

  test("ready is false before /nav resolves and true after", async () => {
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });
    expect(getState().ready).toBe(false);

    await ensureIdentity();

    expect(getState().ready).toBe(true);
  });

  test("derives identity key realm:acme from a mocked /nav", async () => {
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });

    await ensureIdentity();

    expect(getState().identity).toEqual({ scope: "realm", name: "acme" });
  });

  test("a missing cookie entry resolves to member \"\"", async () => {
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });

    await ensureIdentity();

    expect(getState().member).toBe("");
  });

  test("/nav failure leaves identity null, member \"\", ready true", async () => {
    mockNav({ error: "boom" }, false);

    await ensureIdentity();

    expect(getState().identity).toBeNull();
    expect(getState().member).toBe("");
    expect(getState().ready).toBe(true);
  });

  test("never fetches twice per page load — concurrent calls share one in-flight request", async () => {
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });

    await Promise.all([ensureIdentity(), ensureIdentity(), ensureIdentity()]);

    expect((globalThis.fetch as unknown as { mock: { calls: unknown[] } }).mock.calls.length).toBe(1);
  });

  test("setMember writes a cookie entry that a later ensureIdentity reads back", async () => {
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });
    await ensureIdentity();

    setMember("atomic");
    expect(getState().member).toBe("atomic");

    __resetForTest();
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });
    await ensureIdentity();

    expect(getState().member).toBe("atomic");
  });

  test("setMember preserves another identity's cookie entry", async () => {
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });
    await ensureIdentity();
    setMember("atomic");

    __resetForTest();
    mockNav({ scope: "repo", name: "noorm", branch: "", groups: [] });
    await ensureIdentity();
    setMember("api");

    __resetForTest();
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });
    await ensureIdentity();

    expect(getState().member).toBe("atomic");
  });

  test("setMember with no resolved identity updates state and writes no cookie", () => {
    setMember("atomic");

    expect(getState().member).toBe("atomic");
    // No identity was resolved, so no cookie map (JSON, encoded starting
    // "%7B") was ever written — happy-dom leaves an expired cookie's name
    // in `document.cookie` with an empty value rather than removing it.
    expect(document.cookie).not.toContain("%7B");
  });

  test("a malformed cookie reads as {} rather than throwing", async () => {
    document.cookie = "atomic-member=not-json; path=/";
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });

    await ensureIdentity();

    expect(getState().member).toBe("");
    expect(getState().ready).toBe(true);
  });

  test("memberLabel renders the realm name for an empty prefix", () => {
    expect(memberLabel("", "acme")).toBe("acme");
    expect(memberLabel("atomic", "acme")).toBe("atomic");
  });
});
