// NavDrawer — the generated library as an overlay drawer rather than a pane
// that permanently owns a column. Closing it returns that width to the
// document, which is the whole point of the focus-canvas shell.
//
// The drawer stays mounted when closed (hidden via CSS) so the tree keeps its
// fetched data and expanded folders across open/close cycles — unmounting
// would refetch /api/nav and collapse the user's place in the tree every time.
import { useEffect, useRef } from "react";
import { NavTree } from "./NavTree";

export function NavDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const drawerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }

    // Dismiss on any press that lands outside the drawer. The icon rail is
    // excluded: its Browse button is the drawer's own toggle, and closing
    // here would race that toggle into reopening immediately.
    function onPointerDown(e: PointerEvent) {
      const target = e.target;
      if (!(target instanceof Node)) return;
      if (drawerRef.current?.contains(target)) return;
      if (target instanceof Element && target.closest(".icon-rail")) return;
      onClose();
    }

    document.addEventListener("keydown", onKey);
    document.addEventListener("pointerdown", onPointerDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("pointerdown", onPointerDown);
    };
  }, [open, onClose]);

  return (
    <div
      ref={drawerRef}
      className="nav-drawer"
      data-open={open || undefined}
      aria-hidden={!open}
      inert={!open}
    >
      <div className="nav-drawer-head">
        <strong>Browse</strong>
        <kbd className="nav-drawer-hint">Esc</kbd>
      </div>
      <NavTree />
    </div>
  );
}
