// Shell — the three-pane app shell (top bar, left nav, content, rail).
// Visual parity target: templates/layout.html's #shell (pre-cutover markup).
import { useEffect, useState } from "react";
import { Outlet, ScrollRestoration, useNavigate } from "react-router";
import { CodeModal } from "../../components/code-modal/CodeModal";
import { NavTree } from "../../components/nav/NavTree";
import { TopBar } from "../../components/nav/TopBar";
import { PageModal } from "../../components/rail/PageModal";
import { Rail } from "../../components/rail/Rail";
import { SearchPalette } from "../../components/search/SearchPalette";
import { useHashScroll } from "../../hooks/useHashScroll";
import { useLiveReload } from "../../hooks/useLiveReload";
import { installGraphUIGlobal, setNavigator, wireDismiss } from "../../utils/graphUI";
import "./style.css";

export function Shell() {
  useHashScroll();
  const { connState } = useLiveReload();
  const navigate = useNavigate();
  const [searchOpen, setSearchOpen] = useState(false);

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
      <div id="shell">
        <NavTree />
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
