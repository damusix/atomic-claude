import { afterEach, describe, expect, test } from "bun:test";
import { renderHook, waitFor } from "@testing-library/react";
import { events } from "../utils/events";
import { shouldRefetchPage, useLiveReload } from "./useLiveReload";

// A minimal EventSource stand-in — mirrors components/search/useSearchStream
// test's FakeEventSource, plus onopen/readyState/CLOSED since useLiveReload
// (unlike the search stream) drives the connectivity indicator off them.
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  static readonly CLOSED = 2;
  url: string;
  closed = false;
  readyState = 0;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  dispatch(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }

  close() {
    this.closed = true;
  }
}

describe("shouldRefetchPage", () => {
  test("never refetches with no open page", () => {
    expect(shouldRefetchPage(null, ["a.md"])).toBe(false);
    expect(shouldRefetchPage(null, undefined)).toBe(false);
  });

  test("refetches unconditionally when the changed list is omitted (cap exceeded)", () => {
    expect(shouldRefetchPage("a.md", undefined)).toBe(true);
  });

  test("refetches only when the open relpath is in the changed list", () => {
    expect(shouldRefetchPage("a.md", ["a.md", "b.md"])).toBe(true);
    expect(shouldRefetchPage("a.md", ["b.md"])).toBe(false);
    expect(shouldRefetchPage("a.md", [])).toBe(false);
  });
});

describe("useLiveReload", () => {
  afterEach(() => {
    FakeEventSource.instances = [];
    // @ts-expect-error — test-only global stub
    delete globalThis.EventSource;
  });

  test("starts reconnecting, then live on open", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const { result } = renderHook(() => useLiveReload());
    expect(result.current.connState).toBe("reconnecting");

    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const es = FakeEventSource.instances[0]!;
    es.onopen?.();
    await waitFor(() => expect(result.current.connState).toBe("live"));
  });

  test("onerror before CLOSED reports reconnecting; CLOSED reports disconnected", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const { result } = renderHook(() => useLiveReload());
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const es = FakeEventSource.instances[0]!;

    es.readyState = 1; // CONNECTING (retry in flight)
    es.onerror?.();
    await waitFor(() => expect(result.current.connState).toBe("reconnecting"));

    es.readyState = FakeEventSource.CLOSED;
    es.onerror?.();
    await waitFor(() => expect(result.current.connState).toBe("disconnected"));
  });

  test("seeds the first message without emitting realm.changed", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const seen: unknown[] = [];
    const off = events.on("realm.changed", (ev) => seen.push(ev));

    renderHook(() => useLiveReload());
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    FakeEventSource.instances[0]!.dispatch({ fp: "fp1" });

    expect(seen).toEqual([]);
    off();
  });

  test("a same-fp message after the seed is a no-op", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const seen: unknown[] = [];
    const off = events.on("realm.changed", (ev) => seen.push(ev));

    renderHook(() => useLiveReload());
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const es = FakeEventSource.instances[0]!;
    es.dispatch({ fp: "fp1" });
    es.dispatch({ fp: "fp1" });

    expect(seen).toEqual([]);
    off();
  });

  test("a distinct-fp message emits realm.changed with the changed list", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const seen: Array<{ fp: string; changed?: string[] }> = [];
    const off = events.on("realm.changed", (ev) => seen.push(ev));

    renderHook(() => useLiveReload());
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const es = FakeEventSource.instances[0]!;
    es.dispatch({ fp: "fp1" });
    es.dispatch({ fp: "fp2", changed: ["a.md"] });

    expect(seen).toEqual([{ fp: "fp2", changed: ["a.md"] }]);
    off();
  });

  test("an oversized diff omits the changed list — refetch-all per shouldRefetchPage", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const seen: Array<{ fp: string; changed?: string[] }> = [];
    const off = events.on("realm.changed", (ev) => seen.push(ev));

    renderHook(() => useLiveReload());
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const es = FakeEventSource.instances[0]!;
    es.dispatch({ fp: "fp1" });
    es.dispatch({ fp: "fp2" }); // changed omitted (cap exceeded server-side)

    expect(seen).toEqual([{ fp: "fp2" }]);
    expect(shouldRefetchPage("any/page.md", seen[0]?.changed)).toBe(true);
    off();
  });

  test("closes the connection on unmount", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const { unmount } = renderHook(() => useLiveReload());
    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const es = FakeEventSource.instances[0]!;

    unmount();
    expect(es.closed).toBe(true);
  });
});
