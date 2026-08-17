// useHashScroll — owns where the content pane sits after a navigation:
// scrolled to the anchor named by location.hash, or back to the top when
// there isn't one.
//
// Two things make this more than a scrollIntoView call. The scroll container
// is #main-pane, not the window, so react-router's <ScrollRestoration> (which
// only knows about window scroll) leaves a new page showing the previous
// page's offset. And the body arrives asynchronously — the effect runs before
// /api/page resolves, so the anchor element does not exist yet. Re-running on
// "page.headings" is what makes the anchor case reliable: that event fires
// once the injected HTML is in the DOM with its ids assigned.
import { useEffect } from "react";
import { useLocation } from "react-router";
import { events } from "../utils/events";

function mainPane(): HTMLElement | null {
  return document.getElementById("main-pane");
}

function scrollToHash(hash: string): boolean {
  const id = decodeURIComponent(hash.slice(1));
  if (!id) return false;
  const target = document.getElementById(id);
  if (!target) return false;
  target.scrollIntoView({ block: "start" });
  return true;
}

export function useHashScroll(): void {
  const location = useLocation();

  useEffect(() => {
    if (location.hash) {
      // May be too early — the retry below covers the not-yet-rendered case.
      scrollToHash(location.hash);
      return;
    }
    mainPane()?.scrollTo({ top: 0 });
  }, [location.pathname, location.hash]);

  useEffect(() => {
    return events.on("page.headings", () => {
      if (location.hash) {
        scrollToHash(location.hash);
        return;
      }
      mainPane()?.scrollTo({ top: 0 });
    });
  }, [location.pathname, location.hash]);
}
