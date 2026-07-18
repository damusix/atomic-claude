import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiProvider } from "../../utils/api";
import { CodeModal } from "./CodeModal";
import { __resetForTest, closeModal, openFile, openNode } from "./store";

const SOURCE_FIXTURE = {
  html: '<table><tbody><tr id="L1"><td>1</td><td>package main</td></tr><tr id="L2"><td>2</td><td>func main() {}</td></tr></tbody></table>',
  title: "auth.go",
  path: "repo/auth.go",
};

const FILE_INTEL_FIXTURE = {
  path: "repo/auth.go",
  member: "repo",
  nodes: [{ id: "n1", name: "Authenticate", kind: "function", startLine: 1 }],
};

const NODE_FIXTURE = {
  member: "repo",
  node: {
    id: "n1",
    name: "Authenticate",
    kind: "function",
    filePath: "auth.go",
    startLine: 1,
    signature: "func Authenticate() error",
  },
};

const CALLERS_FIXTURE = {
  member: "repo",
  root: NODE_FIXTURE.node,
  edges: [{ kind: "calls", source: "n2", target: "n1" }],
  nodes: {
    n1: NODE_FIXTURE.node,
    n2: { id: "n2", name: "Handler", kind: "function", filePath: "handler.go", startLine: 5 },
  },
};

function mockFetchByUrl(handlers: Record<string, unknown>) {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    for (const [match, body] of Object.entries(handlers)) {
      if (url.includes(match)) {
        return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      }
    }
    return new Response(JSON.stringify({ error: "unexpected path" }), { status: 404 });
  }) as unknown as typeof fetch;
}

function renderModal() {
  return render(
    <ApiProvider>
      <CodeModal />
    </ApiProvider>,
  );
}

describe("CodeModal", () => {
  beforeEach(() => {
    HTMLElement.prototype.scrollIntoView = mock(() => {});
  });

  afterEach(() => {
    __resetForTest();
    closeModal();
    mock.restore();
  });

  test("renders the source pane and scrolls to the anchored line", async () => {
    mockFetchByUrl({ "/api/file/repo/auth.go": SOURCE_FIXTURE, "/api/code/file": FILE_INTEL_FIXTURE });
    renderModal();
    const scrollIntoView = mock(() => {});
    HTMLElement.prototype.scrollIntoView = scrollIntoView;

    act(() => openFile("repo/auth.go", 2));

    await waitFor(() => expect(screen.getByText("func main() {}")).toBeInTheDocument());
    await waitFor(() => expect(scrollIntoView).toHaveBeenCalled());
  });

  test("intel-pane drill-down: file defines -> node detail -> callers", async () => {
    mockFetchByUrl({
      "/api/file/repo/auth.go": SOURCE_FIXTURE,
      "/api/code/file": FILE_INTEL_FIXTURE,
      "/api/code/node": NODE_FIXTURE,
      "/api/code/callers": CALLERS_FIXTURE,
    });
    renderModal();

    act(() => openFile("repo/auth.go"));
    await waitFor(() => expect(screen.getByText("Authenticate")).toBeInTheDocument());

    await userEvent.click(screen.getByText("Authenticate"));
    await waitFor(() => expect(screen.getByText("func Authenticate() error")).toBeInTheDocument());

    await userEvent.click(screen.getByText("callers"));
    await waitFor(() => expect(screen.getByText("Handler")).toBeInTheDocument());
  });

  test("back-stack: Back pops to the previous intel view", async () => {
    mockFetchByUrl({
      "/api/file/repo/auth.go": SOURCE_FIXTURE,
      "/api/code/file": FILE_INTEL_FIXTURE,
      "/api/code/node": NODE_FIXTURE,
    });
    renderModal();

    act(() => openFile("repo/auth.go"));
    await waitFor(() => expect(screen.getByText("Authenticate")).toBeInTheDocument());
    expect(screen.getByText("← Back")).not.toBeVisible();

    await userEvent.click(screen.getByText("Authenticate"));
    await waitFor(() => expect(screen.getByText("func Authenticate() error")).toBeInTheDocument());
    expect(screen.getByText("← Back")).toBeVisible();

    await userEvent.click(screen.getByText("← Back"));
    await waitFor(() => expect(screen.getByText("Defines (1)")).toBeInTheDocument());
    expect(screen.getByText("← Back")).not.toBeVisible();
  });

  test("dedup: drilling to a node in the same file does not re-fetch the source pane", async () => {
    mockFetchByUrl({
      "/api/file/repo/auth.go": SOURCE_FIXTURE,
      "/api/code/file": FILE_INTEL_FIXTURE,
      "/api/code/node": NODE_FIXTURE,
    });
    renderModal();

    act(() => openFile("repo/auth.go"));
    await waitFor(() => expect(screen.getByText("Authenticate")).toBeInTheDocument());
    const fetchCountAfterOpen = (globalThis.fetch as unknown as { mock: { calls: unknown[] } }).mock.calls.length;

    await userEvent.click(screen.getByText("Authenticate"));
    await waitFor(() => expect(screen.getByText("func Authenticate() error")).toBeInTheDocument());

    const sourceFetches = (globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } }).mock.calls
      .slice(fetchCountAfterOpen)
      .filter(([input]) => (typeof input === "string" ? input : input.toString()).includes("/api/file/"));
    expect(sourceFetches).toHaveLength(0);
  });

  test("closing the modal clears the stack", async () => {
    mockFetchByUrl({ "/api/file/repo/auth.go": SOURCE_FIXTURE, "/api/code/file": FILE_INTEL_FIXTURE });
    renderModal();

    act(() => openFile("repo/auth.go"));
    await waitFor(() => expect(screen.getByText("Authenticate")).toBeInTheDocument());

    await userEvent.click(screen.getByLabelText("Close"));
    await waitFor(() => expect(screen.queryByText("Authenticate")).not.toBeInTheDocument());
  });

  test("openNode (window.AtomicCodeExplorer bridge) opens with member-prefixed source path", async () => {
    mockFetchByUrl({ "/api/file/repo/auth.go": SOURCE_FIXTURE, "/api/code/node": NODE_FIXTURE });
    renderModal();

    act(() => openNode("n1", "repo", { title: "Authenticate", file: "auth.go", line: 1 }));

    await waitFor(() => expect(screen.getByText("func main() {}")).toBeInTheDocument());
    expect(screen.getAllByText("Authenticate").length).toBeGreaterThan(0);
  });
});
