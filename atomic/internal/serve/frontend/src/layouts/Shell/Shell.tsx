// Shell — the three-pane app shell (top bar, left nav, content, rail).
// Visual parity target: templates/layout.html's #shell (pre-cutover markup).
import { Outlet, ScrollRestoration } from "react-router";
import { NavTree } from "../../components/nav/NavTree";
import { TopBar } from "../../components/nav/TopBar";
import { useHashScroll } from "../../hooks/useHashScroll";
import "./style.css";

export function Shell() {
  useHashScroll();

  return (
    <>
      <TopBar />
      <div id="shell">
        <NavTree />
        <div id="content-column">
          <main id="main-pane">
            <Outlet />
          </main>
        </div>
        {/* Properties/mini-graph/OUT/IN panels land in CP6. */}
        <aside id="right-rail" aria-label="Rail" />
      </div>
      <ScrollRestoration />
    </>
  );
}
