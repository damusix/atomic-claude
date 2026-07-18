// useHashScroll — scrolls the element named by `location.hash` into view on
// mount and whenever the route (path or hash) changes. Complements
// react-router's <ScrollRestoration> (Shell), which restores the scroll
// offset on Back/Forward but has no opinion on same-navigation anchors.
import { useEffect } from "react";
import { useLocation } from "react-router";

export function useHashScroll(): void {
  const location = useLocation();

  useEffect(() => {
    if (!location.hash) return;
    const id = decodeURIComponent(location.hash.slice(1));
    document.getElementById(id)?.scrollIntoView();
  }, [location.pathname, location.hash]);
}
