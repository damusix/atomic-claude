// utils/graphUI — rebuild of templates/layout.html's window.AtomicGraphUI
// shared block: hover preview card + page modal + navigate. Engine-neutral
// (a plain node-data object in, never a Cytoscape/cosmos node) so both the
// rail mini-graph (components/rail) and the carried system-graph.js/
// code-graph.js profiles (CP8) share one implementation — same contract,
// same DOM elements, same CSS (app.css's #cy-preview-card / #cy-page-modal-scrim
// / .cy-modal* selectors, carried verbatim).
//
// Exposed on window.AtomicGraphUI under the same member names the carried
// vanilla <script>s already call unqualified (verified call sites:
// system-graph.js, code-graph.js): showPreviewCard, hidePreviewCard,
// openPageModal, closePageModal, navigateToPage, setNavigator, wireDismiss.
//
// Default navigation has no htmx to swap through — instead a navigator
// function is registered by the Shell (React Router's useNavigate), the
// same seam the original used for the system-mode-aware navigateHook
// override (CP8 wires its own teardown-first navigator the same way).
import { attempt } from "@logosdx/utils";
import { api } from "./api";

export interface GraphUINodeData {
  type?: string;
  title?: string;
  label?: string;
  description?: string;
  snippet?: string;
}

export type PageNavigator = (nodeID: string) => void;

let navigateHook: PageNavigator | null = null;
let dismissWired = false;

function escapeHTML(s: string): string {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function getOrCreatePreviewCard(): HTMLElement {
  let el = document.getElementById("cy-preview-card");
  if (!el) {
    el = document.createElement("div");
    el.id = "cy-preview-card";
    document.body.appendChild(el);
  }
  return el;
}

// showPreviewCard renders the hover preview for a node. screenPos must
// already be in containerEl's own coordinate frame (top-left origin) — each
// engine converts with its own primitive (Cytoscape's renderedPosition(),
// cosmos's spaceToScreenPosition()) before calling here.
/**
 * `openTarget` turns the card into something you can act on rather than only
 * read: it appends an Open button that navigates to that page. Callers that
 * show the card on hover leave it unset — a button you have to move the
 * pointer onto is unreachable when leaving the node dismisses the card.
 */
export function showPreviewCard(
  node: GraphUINodeData,
  screenPos: { x: number; y: number },
  containerEl?: HTMLElement | null,
  openTarget?: string,
): void {
  const card = getOrCreatePreviewCard();
  const type = node.type || "page";
  const title = node.title || node.label || "";
  const desc = node.description || "";
  const snip = node.snippet || "";

  let html = `<span class="cy-pc-chip ${type}">${escapeHTML(type)}</span><div class="cy-pc-title">${escapeHTML(title)}</div>`;
  if (desc) html += `<div class="cy-pc-desc">${escapeHTML(desc)}</div>`;
  if (snip) html += `<div class="cy-pc-snip">${escapeHTML(snip)}</div>`;
  if (openTarget) html += `<button type="button" class="cy-pc-open">Open page</button>`;
  card.innerHTML = html;
  if (openTarget) {
    // Wired after innerHTML, which discards any listener bound to the
    // previous contents.
    card.querySelector(".cy-pc-open")?.addEventListener("click", () => {
      navigateToPage(openTarget);
    });
  }

  const container = containerEl || document.getElementById("main-pane");
  const rect = container?.getBoundingClientRect() ?? { left: 0, top: 0 };
  const cardW = 284;
  const cardH = card.offsetHeight || 170;
  const gap = 14;
  let left = rect.left + screenPos.x + gap;
  let top = rect.top + screenPos.y + gap;
  if (left + cardW + 8 > window.innerWidth) left = rect.left + screenPos.x - cardW - gap;
  if (left < 8) left = 8;
  if (top + cardH + 8 > window.innerHeight) top = window.innerHeight - cardH - 8;
  if (top < 8) top = 8;
  card.style.left = `${left}px`;
  card.style.top = `${top}px`;
  card.classList.add("open");
}

export function hidePreviewCard(): void {
  document.getElementById("cy-preview-card")?.classList.remove("open");
}

// openPageModal loads the target page's rendered HTML into the modal body via
// /api/page (JSON), replacing the original's raw fetch('/page/…', {headers:
// {'HX-Request': 'true'}}).then(r => r.text()) with an attempt() tuple over
// the JSON endpoint. nodeData is optional (falsy when the caller has no
// pre-resolved metadata) — the modal chip/title/desc fall back to nodeID.
export function openPageModal(nodeID: string, nodeData?: GraphUINodeData | null): void {
  const scrim = document.getElementById("cy-page-modal-scrim");
  if (!scrim) return;
  const modalBody = document.getElementById("cy-modal-body");
  const modalChip = document.getElementById("cy-modal-chip");
  const modalTitle = document.getElementById("cy-modal-title");
  const modalDesc = document.getElementById("cy-modal-desc");
  const openBtn = document.getElementById("cy-modal-open-btn");

  const type = nodeData?.type || "page";
  const title = nodeData?.title || nodeData?.label || nodeID;
  const desc = nodeData?.description || "";

  if (modalChip) {
    modalChip.className = `cy-modal-chip ${type}`;
    modalChip.textContent = type;
  }
  if (modalTitle) modalTitle.textContent = title;
  if (modalDesc) {
    modalDesc.textContent = desc;
    modalDesc.style.display = desc ? "" : "none";
  }
  if (openBtn) openBtn.onclick = () => navigateToPage(nodeID);

  if (modalBody) {
    modalBody.innerHTML = '<p class="loading">Loading…</p>';
    void attempt(() => api.get<{ html: string }>(`/page/${encodeURIComponent(nodeID)}`)).then(
      ([res, err]) => {
        if (err || !res?.ok || !res.data) {
          modalBody.innerHTML =
            '<p style="color:var(--ink-faint);font-style:italic">Could not load page.</p>';
          return;
        }
        modalBody.innerHTML = res.data.html;
        modalBody.scrollTop = 0;
      },
    );
  }

  scrim.classList.add("open");
}

export function closePageModal(): void {
  document.getElementById("cy-page-modal-scrim")?.classList.remove("open");
}

// navigateToPage closes the modal + preview card, then delegates to the
// registered navigator (Shell wires React Router's navigate on mount) or
// falls back to a full navigation when none is registered yet.
export function navigateToPage(nodeID: string): void {
  closePageModal();
  hidePreviewCard();
  if (navigateHook) {
    navigateHook(nodeID);
    return;
  }
  window.location.assign(`/page/${encodeURIComponent(nodeID)}`);
}

export function setNavigator(fn: PageNavigator | null): void {
  navigateHook = fn;
}

// wireDismiss registers modal-dismiss wiring exactly once: scrim backdrop,
// corner ×, bottom Close button, Esc. Idempotent — safe to call from every
// mount site (rail, CP8's graph views) without double-binding listeners.
export function wireDismiss(): void {
  if (dismissWired) return;
  dismissWired = true;
  const scrim = document.getElementById("cy-page-modal-scrim");
  scrim?.addEventListener("click", (e) => {
    if (e.target === scrim) closePageModal();
  });
  document.getElementById("cy-modal-close-btn")?.addEventListener("click", closePageModal);
  document.getElementById("cy-modal-dismiss-btn")?.addEventListener("click", closePageModal);
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    document.getElementById("cy-page-modal-scrim")?.classList.remove("open");
  });
}

// Test-only reset — clears module-level state (navigator, dismiss-wired
// flag) between test cases so wireDismiss()'s idempotence guard doesn't leak
// across tests that render fresh scrim/button elements each time.
export function __resetForTest(): void {
  navigateHook = null;
  dismissWired = false;
}

export const AtomicGraphUI = {
  showPreviewCard,
  hidePreviewCard,
  openPageModal,
  closePageModal,
  navigateToPage,
  setNavigator,
  wireDismiss,
};

export function installGraphUIGlobal(target: Window = window): void {
  target.AtomicGraphUI = AtomicGraphUI;
}

declare global {
  interface Window {
    AtomicGraphUI: typeof AtomicGraphUI;
  }
}
