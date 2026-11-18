// MermaidLightbox — click a rendered diagram to zoom and pan it over the page.
// Mounted once by Shell; the docs site's .vitepress/theme/mermaid-zoom.ts on Ark Dialog.
import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from "react";
import { Dialog } from "@ark-ui/react";
import { closeDiagram, getDiagram, installDiagramClicks, subscribe } from "./store";
import { attachViewer, newViewState } from "./viewer";
import "./style.css";

export function MermaidLightbox() {
  const diagram = useSyncExternalStore(subscribe, getDiagram, getDiagram);
  const markup = diagram?.markup ?? "";
  const html = useMemo(() => ({ __html: markup }), [markup]);
  const view = useMemo(() => (diagram ? newViewState() : null), [diagram]);

  useEffect(() => installDiagramClicks(), []);

  // Focus lands on the viewport so Escape works at once without drawing a
  // focus ring around the close button on a mouse open.
  const viewportEl = useRef<HTMLDivElement | null>(null);

  // A callback ref, not an effect: Ark mounts the content after the open state
  // commits, so an effect keyed on `diagram` can run before the viewport exists.
  const viewportRef = useCallback(
    (viewport: HTMLDivElement | null) => {
      viewportEl.current = viewport;
      const stage = viewport?.querySelector<HTMLElement>(".mermaid-lightbox-stage");
      const svg = stage?.querySelector("svg");
      if (!viewport || !stage || !svg || !diagram || !view) return;
      svg.style.width = `${diagram.width}px`;
      svg.style.height = `${diagram.height}px`;
      svg.style.maxWidth = "none";
      return attachViewer(viewport, stage, diagram, closeDiagram, view);
    },
    [diagram, view],
  );

  return (
    <Dialog.Root
      open={diagram !== null}
      onOpenChange={(details) => {
        if (!details.open) closeDiagram();
      }}
      lazyMount
      unmountOnExit
      initialFocusEl={() => viewportEl.current}
      aria-label="Diagram viewer"
    >
      <Dialog.Positioner className="mermaid-lightbox">
        <Dialog.Content className="mermaid-lightbox-viewport" ref={viewportRef}>
          <div
            className="mermaid-lightbox-stage"
            // eslint-disable-next-line react/no-danger -- mermaid's own SVG, already in the page
            dangerouslySetInnerHTML={html}
          />
          <Dialog.CloseTrigger className="mermaid-lightbox-close" aria-label="Close diagram viewer">
            ×
          </Dialog.CloseTrigger>
          <p className="mermaid-lightbox-hint">
            scroll, pinch or +/− to zoom · drag to pan · double-click or 0 to reset · esc to close
          </p>
        </Dialog.Content>
      </Dialog.Positioner>
    </Dialog.Root>
  );
}
