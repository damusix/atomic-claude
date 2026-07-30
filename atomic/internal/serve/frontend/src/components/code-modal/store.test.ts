import { afterEach, describe, expect, test } from "bun:test";
import {
  __resetForTest,
  closeModal,
  getState,
  installCodeExplorerGlobal,
  openFile,
  openNode,
  popIntel,
  pushIntel,
  subscribe,
} from "./store";

describe("code-modal store", () => {
  afterEach(() => {
    __resetForTest();
  });

  test("openFile seeds the stack with a file intel target and opens the modal", () => {
    openFile("atomic/internal/serve/render.go", 42);
    expect(getState()).toEqual({
      open: true,
      stack: [
        {
          filePath: "atomic/internal/serve/render.go",
          line: 42,
          title: "atomic/internal/serve/render.go",
          intel: { kind: "file", path: "atomic/internal/serve/render.go" },
        },
      ],
    });
  });

  test("openNode joins the member prefix onto meta.file for the source pane path", () => {
    openNode("n1", "repo", { title: "Authenticate", file: "auth.go", line: 10 });
    expect(getState().stack).toEqual([
      {
        filePath: "repo/auth.go",
        line: 10,
        title: "Authenticate",
        intel: { kind: "node", id: "n1", member: "repo" },
      },
    ]);
  });

  test("openNode with no member leaves the path unprefixed", () => {
    openNode("n1", "", { title: "Authenticate", file: "auth.go", line: 10 });
    expect(getState().stack[0]?.filePath).toBe("auth.go");
  });

  test("openNode with no meta.file location has a null filePath", () => {
    openNode("n1", "repo", null);
    expect(getState().stack[0]?.filePath).toBeNull();
  });

  test("pushIntel appends onto the back-stack without disturbing earlier entries", () => {
    openFile("a.go");
    pushIntel({ filePath: "a.go", line: 5, title: "Foo", intel: { kind: "node", id: "n1", member: "" } });
    expect(getState().stack).toHaveLength(2);
    expect(getState().stack[0]?.intel).toEqual({ kind: "file", path: "a.go" });
    expect(getState().stack[1]?.title).toBe("Foo");
  });

  test("popIntel pops the top entry, reverting to the previous view", () => {
    openFile("a.go");
    pushIntel({ filePath: "a.go", line: 5, title: "Foo", intel: { kind: "node", id: "n1", member: "" } });
    popIntel();
    expect(getState().stack).toHaveLength(1);
    expect(getState().stack[0]?.intel).toEqual({ kind: "file", path: "a.go" });
  });

  test("popIntel at the root is a no-op", () => {
    openFile("a.go");
    popIntel();
    expect(getState().stack).toHaveLength(1);
  });

  test("closeModal clears the stack and closes", () => {
    openFile("a.go");
    closeModal();
    expect(getState()).toEqual({ open: false, stack: [] });
  });

  test("subscribe notifies listeners on every state change", () => {
    let calls = 0;
    const unsubscribe = subscribe(() => {
      calls += 1;
    });
    openFile("a.go");
    pushIntel({ filePath: "a.go", line: null, title: "x", intel: { kind: "node", id: "n1", member: "" } });
    popIntel();
    closeModal();
    unsubscribe();
    expect(calls).toBe(4);
  });

  test("installCodeExplorerGlobal exposes window.AtomicCodeExplorer.openNode with the code-graph.js call signature", () => {
    const fakeWindow = {} as unknown as Window;
    installCodeExplorerGlobal(fakeWindow);
    (fakeWindow as unknown as { AtomicCodeExplorer: { openNode: typeof openNode } }).AtomicCodeExplorer.openNode(
      "n1",
      "repo",
      { title: "Foo", file: "a.go", line: 1 },
    );
    expect(getState().stack[0]?.title).toBe("Foo");
  });
});
