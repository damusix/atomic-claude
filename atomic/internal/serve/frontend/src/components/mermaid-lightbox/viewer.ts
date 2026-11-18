// viewer — wheel/pinch/key zoom, drag pan, double-click fit, and click-off close
// for the lightbox. attachViewer wires one session and returns its teardown.

const SCALE_MIN = 0.1;
const SCALE_MAX = 40;
const CLICK_SLOP_PX = 5;
const FIT_MARGIN = 0.92;
const KEY_ZOOM = 1.2;

interface Point {
  x: number;
  y: number;
}

// Outlives a session: the dialog re-attaches on every re-render of its subtree,
// and a fresh state would snap a zoomed view back to fit each time.
export interface ViewState {
  scale: number;
  tx: number;
  ty: number;
  placed: boolean;
}

export function newViewState(): ViewState {
  return { scale: 1, tx: 0, ty: 0, placed: false };
}

export function attachViewer(
  viewport: HTMLElement,
  stage: HTMLElement,
  size: { width: number; height: number },
  close: () => void,
  view: ViewState,
): () => void {
  const apply = () => {
    stage.style.transform = `translate(${view.tx}px, ${view.ty}px) scale(${view.scale})`;
  };

  const fit = () => {
    view.scale = Math.min((window.innerWidth * FIT_MARGIN) / size.width, (window.innerHeight * FIT_MARGIN) / size.height);
    view.tx = (window.innerWidth - size.width * view.scale) / 2;
    view.ty = (window.innerHeight - size.height * view.scale) / 2;
    view.placed = true;
    apply();
  };

  const zoomAt = (clientX: number, clientY: number, factor: number) => {
    const next = Math.min(SCALE_MAX, Math.max(SCALE_MIN, view.scale * factor));
    const applied = next / view.scale;
    view.tx = clientX - (clientX - view.tx) * applied;
    view.ty = clientY - (clientY - view.ty) * applied;
    view.scale = next;
    apply();
  };

  const pointers = new Map<number, Point>();
  let dragDistance = 0;
  // Read at press time: pointer capture retargets the click to the viewport.
  let pressedOnDrawing = false;

  const onWheel = (event: WheelEvent) => {
    // preventDefault needs a non-passive listener. ctrl+wheel arrives the same
    // way, which is what makes a trackpad pinch zoom.
    event.preventDefault();
    zoomAt(event.clientX, event.clientY, Math.exp(-event.deltaY * 0.002));
  };

  const onKeyDown = (event: KeyboardEvent) => {
    const cx = window.innerWidth / 2;
    const cy = window.innerHeight / 2;
    if (event.key === "+" || event.key === "=") zoomAt(cx, cy, KEY_ZOOM);
    else if (event.key === "-") zoomAt(cx, cy, 1 / KEY_ZOOM);
    else if (event.key === "0") fit();
    else return;
    event.preventDefault();
  };

  const onPointerDown = (event: PointerEvent) => {
    if (event.target instanceof Element && event.target.closest("button")) return;
    viewport.setPointerCapture(event.pointerId);
    pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (pointers.size === 1) {
      dragDistance = 0;
      pressedOnDrawing = event.target instanceof Element && event.target.closest("svg") !== null;
    }
  };

  const onPointerMove = (event: PointerEvent) => {
    const previous = pointers.get(event.pointerId);
    if (!previous) return;
    const current = { x: event.clientX, y: event.clientY };

    if (pointers.size === 1) {
      view.tx += current.x - previous.x;
      view.ty += current.y - previous.y;
      dragDistance += Math.hypot(current.x - previous.x, current.y - previous.y);
      apply();
    } else if (pointers.size === 2) {
      const anchor = [...pointers.entries()].find(([id]) => id !== event.pointerId)?.[1];
      if (anchor) {
        const before = Math.hypot(previous.x - anchor.x, previous.y - anchor.y);
        const after = Math.hypot(current.x - anchor.x, current.y - anchor.y);
        if (before > 0) zoomAt((current.x + anchor.x) / 2, (current.y + anchor.y) / 2, after / before);
      }
      dragDistance = Number.POSITIVE_INFINITY;
    }
    pointers.set(event.pointerId, current);
  };

  const onPointerEnd = (event: PointerEvent) => {
    pointers.delete(event.pointerId);
  };

  // A click on empty space closes, like clicking off a modal; a click that
  // ended a drag, or landed on the drawing, does not.
  const onClick = (event: MouseEvent) => {
    if (dragDistance > CLICK_SLOP_PX || pressedOnDrawing) return;
    if (event.target instanceof Element && event.target.closest("button")) return;
    close();
  };

  viewport.addEventListener("wheel", onWheel, { passive: false });
  viewport.addEventListener("keydown", onKeyDown);
  viewport.addEventListener("pointerdown", onPointerDown);
  viewport.addEventListener("pointermove", onPointerMove);
  viewport.addEventListener("pointerup", onPointerEnd);
  viewport.addEventListener("pointercancel", onPointerEnd);
  viewport.addEventListener("dblclick", fit);
  viewport.addEventListener("click", onClick);
  window.addEventListener("resize", fit);
  if (view.placed) apply();
  else fit();

  return () => {
    viewport.removeEventListener("wheel", onWheel);
    viewport.removeEventListener("keydown", onKeyDown);
    viewport.removeEventListener("pointerdown", onPointerDown);
    viewport.removeEventListener("pointermove", onPointerMove);
    viewport.removeEventListener("pointerup", onPointerEnd);
    viewport.removeEventListener("pointercancel", onPointerEnd);
    viewport.removeEventListener("dblclick", fit);
    viewport.removeEventListener("click", onClick);
    window.removeEventListener("resize", fit);
  };
}
