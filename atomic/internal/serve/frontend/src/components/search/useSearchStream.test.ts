import { afterEach, describe, expect, test } from "bun:test";
import { renderHook, waitFor } from "@testing-library/react";
import { useSearchStream } from "./useSearchStream";

// A minimal EventSource stand-in — captures listeners registered per named
// event so the test can dispatch them directly, mirroring how the browser
// would invoke them as SSE frames arrive.
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  closed = false;
  onerror: (() => void) | null = null;
  private listeners: Record<string, ((e: { data: string }) => void)[]> = {};

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(event: string, cb: (e: { data: string }) => void) {
    (this.listeners[event] ??= []).push(cb);
  }

  dispatch(event: string, data: unknown) {
    for (const cb of this.listeners[event] ?? []) cb({ data: JSON.stringify(data) });
  }

  close() {
    this.closed = true;
  }
}

describe("useSearchStream", () => {
  afterEach(() => {
    FakeEventSource.instances = [];
    // @ts-expect-error — test-only global stub
    delete globalThis.EventSource;
  });

  test("stays idle with no query", () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const { result } = renderHook(() => useSearchStream("", "all"));
    expect(result.current).toEqual({ md: null, code: [], done: true });
    expect(FakeEventSource.instances.length).toBe(0);
  });

  test("opens a stream and accumulates md/code events until end", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const { result } = renderHook(() => useSearchStream("auth", "all"));

    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const es = FakeEventSource.instances[0]!;
    expect(es.url).toContain("/api/search/stream?q=auth&src=all");
    expect(result.current.done).toBe(false);

    es.dispatch("md", { query: "auth", truncated: false, cap: 50, results: [{ relpath: "a.md", line: 1, snippet: "auth" }] });
    await waitFor(() => expect(result.current.md?.results.length).toBe(1));

    es.dispatch("code", {
      member: { key: "repo", prefix: "repo", indexed: true },
      results: [{ id: "n1", name: "Auth", kind: "func", filePath: "auth.go", startLine: 10 }],
    });
    await waitFor(() => expect(result.current.code.length).toBe(1));

    es.dispatch("code", { member: { key: "cold", prefix: "cold", indexed: false }, results: [] });
    await waitFor(() => expect(result.current.code.length).toBe(2));
    expect(result.current.code[1]?.member.indexed).toBe(false);

    expect(result.current.done).toBe(false);
    es.dispatch("end", {});
    await waitFor(() => expect(result.current.done).toBe(true));
    expect(es.closed).toBe(true);
  });

  test("closes and marks done on a transport error", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    const { result } = renderHook(() => useSearchStream("auth", "all"));

    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const es = FakeEventSource.instances[0]!;
    es.onerror?.();

    await waitFor(() => expect(result.current.done).toBe(true));
    expect(es.closed).toBe(true);
  });
});
