// Shell — the focus-canvas app shell: top bar, permanent icon rail, the
// generated library in an overlay drawer, content, inspector rail. The drawer
// (rather than a fourth permanent column) is what lets the document own the
// full width while reading, which is the whole point of the layout.
import { useCallback, useEffect, useState } from "react";
import { Outlet, ScrollRestoration, useNavigate } from "react-router";
import { CodeModal } from "../../components/code-modal/CodeModal";
import { IconRail } from "../../components/nav/IconRail";
import { NavDrawer } from "../../components/nav/NavDrawer";
import { TopBar } from "../../components/nav/TopBar";
import { PageModal } from "../../components/rail/PageModal";
import { Rail } from "../../components/rail/Rail";
import { SearchPalette } from "../../components/search/SearchPalette";
import { useFavicon } from "../../hooks/useFavicon";
import { useGraphWarm } from "../../hooks/useGraphWarm";
import { useHashScroll } from "../../hooks/useHashScroll";
import { useLiveReload } from "../../hooks/useLiveReload";
import { installGraphUIGlobal, setNavigator, wireDismiss } from "../../utils/graphUI";
import "./style.css";

const NAV_KEY = "atomic-nav-open";

export function Shell() {
  useHashScroll();
  const { connState } = useLiveReload();
  useFavicon(connState);
  useGraphWarm();
  const navigate = useNavigate();
  const [searchOpen, setSearchOpen] = useState(false);
  // Drawer state persists so the shell reopens the way it was left — a
  // reader who keeps the library open does not re-open it every reload.
  const [navOpen, setNavOpen] = useState(() => {
    if (typeof localStorage === "undefined") return false;
    return localStorage.getItem(NAV_KEY) === "open";
  });

  const toggleNav = useCallback(() => {
    setNavOpen((open) => {
      const next = !open;
      if (typeof localStorage !== "undefined") {
        localStorage.setItem(NAV_KEY, next ? "open" : "closed");
      }
      return next;
    });
  }, []);

  const closeNav = useCallback(() => {
    setNavOpen(false);
    if (typeof localStorage !== "undefined") localStorage.setItem(NAV_KEY, "closed");
  }, []);

  // Exposes window.AtomicGraphUI so the carried vanilla profiles
  // (system-graph.js, code-graph.js) can call showPreviewCard/openPageModal/
  // etc. unqualified; registers the SPA navigator utils/graphUI's
  // navigateToPage delegates to (rail mini-graph clicks, the page modal's
  // "Open full page" button, and CP8's graph-view click handlers all funnel
  // through the one AtomicGraphUI contract) and wires the modal's dismiss
  // handlers once.
  useEffect(() => {
    installGraphUIGlobal();
    setNavigator((nodeID) => navigate(`/page/${nodeID}`));
    wireDismiss();
    return () => setNavigator(null);
  }, [navigate]);

  return (
    <>
      <TopBar connState={connState} onOpenSearch={() => setSearchOpen(true)} />
      <div id="shell" data-nav={navOpen ? "open" : "closed"}>
        <IconRail navOpen={navOpen} onToggleNav={toggleNav} />
        <NavDrawer open={navOpen} onClose={closeNav} />
        <div id="content-column">
          <main id="main-pane">
            <Outlet />
          </main>
        </div>
        <Rail />
      </div>
      <PageModal />
      <CodeModal />
      <SearchPalette open={searchOpen} onOpenChange={setSearchOpen} />
      <ScrollRestoration />
    </>
  );
}
