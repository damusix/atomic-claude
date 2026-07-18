import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import {
  __resetForTest,
  AtomicGraphUI,
  closePageModal,
  hidePreviewCard,
  installGraphUIGlobal,
  navigateToPage,
  openPageModal,
  setNavigator,
  showPreviewCard,
  wireDismiss,
} from "./graphUI";

function mountModalDOM() {
  document.body.innerHTML = `
    <div id="cy-page-modal-scrim">
      <div class="cy-modal">
        <button id="cy-modal-close-btn"></button>
        <span id="cy-modal-chip"></span>
        <div id="cy-modal-title"></div>
        <div id="cy-modal-desc"></div>
        <div id="cy-modal-body"></div>
        <button id="cy-modal-open-btn"></button>
        <button id="cy-modal-dismiss-btn"></button>
      </div>
    </div>
    <div id="main-pane"></div>
  `;
}

function mockFetchOnce(body: unknown) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

describe("utils/graphUI", () => {
  beforeEach(() => {
    __resetForTest();
    mountModalDOM();
  });

  afterEach(() => {
    mock.restore();
    document.body.innerHTML = "";
  });

  test("installGraphUIGlobal exposes the exact contract carried system-graph.js/code-graph.js call", () => {
    installGraphUIGlobal(window);
    expect(window.AtomicGraphUI).toBe(AtomicGraphUI);
    for (const member of [
      "showPreviewCard",
      "hidePreviewCard",
      "openPageModal",
      "closePageModal",
      "navigateToPage",
      "setNavigator",
      "wireDismiss",
    ] as const) {
      expect(typeof window.AtomicGraphUI[member]).toBe("function");
    }
  });

  test("showPreviewCard creates the card once, sets content, and opens it", () => {
    showPreviewCard(
      { type: "repo", title: "My Repo", description: "desc", snippet: "snip" },
      { x: 10, y: 10 },
    );
    const card = document.getElementById("cy-preview-card");
    expect(card).not.toBeNull();
    expect(card?.classList.contains("open")).toBe(true);
    expect(card?.innerHTML).toContain("My Repo");
    expect(card?.innerHTML).toContain("desc");
  });

  test("hidePreviewCard removes the open class", () => {
    showPreviewCard({ title: "x" }, { x: 0, y: 0 });
    hidePreviewCard();
    expect(document.getElementById("cy-preview-card")?.classList.contains("open")).toBe(false);
  });

  test("openPageModal fills chip/title/desc and fetches /page/<id> for the body", async () => {
    mockFetchOnce({ html: "<p>loaded body</p>" });
    openPageModal("wiki/index.md", { type: "knowledge", title: "Index" });

    expect(document.getElementById("cy-modal-chip")?.textContent).toBe("knowledge");
    expect(document.getElementById("cy-modal-title")?.textContent).toBe("Index");
    expect(document.getElementById("cy-page-modal-scrim")?.classList.contains("open")).toBe(true);

    await new Promise((r) => setTimeout(r, 0));
    expect(document.getElementById("cy-modal-body")?.innerHTML).toBe("<p>loaded body</p>");
  });

  test("closePageModal removes the open class", () => {
    openPageModal("x");
    closePageModal();
    expect(document.getElementById("cy-page-modal-scrim")?.classList.contains("open")).toBe(false);
  });

  test("navigateToPage delegates to the registered navigator and closes the modal/card first", () => {
    openPageModal("a");
    showPreviewCard({ title: "a" }, { x: 0, y: 0 });

    const seen: string[] = [];
    setNavigator((id) => seen.push(id));
    navigateToPage("wiki/other.md");

    expect(seen).toEqual(["wiki/other.md"]);
    expect(document.getElementById("cy-page-modal-scrim")?.classList.contains("open")).toBe(false);
    expect(document.getElementById("cy-preview-card")?.classList.contains("open")).toBe(false);
  });

  test("navigateToPage falls back to a full navigation when no navigator is registered", () => {
    const assign = mock(() => {});
    // happy-dom's window.location.assign — stub it to observe the fallback
    // without actually navigating.
    (window.location as unknown as { assign: typeof assign }).assign = assign;

    navigateToPage("readme.md");
    expect(assign).toHaveBeenCalledWith("/page/readme.md");
  });

  test("wireDismiss registers Esc-to-close, and is safe to call more than once", () => {
    document.getElementById("cy-page-modal-scrim")!.classList.add("open");

    wireDismiss();
    wireDismiss(); // idempotence guard — must not throw or double-register

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(document.getElementById("cy-page-modal-scrim")?.classList.contains("open")).toBe(false);
  });

  test("wireDismiss binds the close (x) and dismiss (Close) buttons", () => {
    document.getElementById("cy-page-modal-scrim")!.classList.add("open");
    wireDismiss();

    document.getElementById("cy-modal-close-btn")?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(document.getElementById("cy-page-modal-scrim")?.classList.contains("open")).toBe(false);

    document.getElementById("cy-page-modal-scrim")!.classList.add("open");
    document.getElementById("cy-modal-dismiss-btn")?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(document.getElementById("cy-page-modal-scrim")?.classList.contains("open")).toBe(false);
  });
});
