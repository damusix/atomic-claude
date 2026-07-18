// TopBar — brand, breadcrumb, search trigger, connection indicator, theme
// toggle. Visual parity target: templates/layout.html's #app-header (pre-
// cutover markup, still the source of truth for structure/classes until the
// cutover checkpoint deletes it).
import { Link, useLocation } from "react-router";
import type { ConnState } from "../../hooks/useLiveReload";
import { useTheme } from "../../hooks/useTheme";
import "./style.css";

export type { ConnState };

const CONN_LABEL: Record<ConnState, string> = {
  live: "live",
  reconnecting: "connecting…",
  disconnected: "disconnected",
};

function Breadcrumb() {
  const location = useLocation();
  const segments = location.pathname.replace(/^\/page\//, "").split("/").filter(Boolean);
  const label = segments.length > 0 ? segments[segments.length - 1] : "home";

  return (
    <nav className="breadcrumb" aria-label="Breadcrumb">
      <span className="breadcrumb-scope">atomic</span>
      <span className="breadcrumb-sep">›</span>
      <span className="breadcrumb-page">{label}</span>
    </nav>
  );
}

export function TopBar({
  connState = "reconnecting",
  onOpenSearch,
}: {
  connState?: ConnState;
  onOpenSearch?: () => void;
}) {
  const { theme, toggle } = useTheme();

  return (
    <header id="app-header">
      <Link className="brand" to="/" aria-label="atomic — home">
        atomic
      </Link>
      <Breadcrumb />
      <div className="search-bar">
        <button
          type="button"
          className="search-trigger"
          aria-haspopup="dialog"
          aria-label="Open search (Command K)"
          onClick={onOpenSearch}
        >
          <span className="search-trigger-label">Search…</span>
          <kbd className="search-trigger-kbd">⌘K</kbd>
        </button>
      </div>
      <span
        className="conn-indicator"
        data-conn-state={connState}
        title={`Live reload: ${CONN_LABEL[connState]}`}
      >
        <span className="conn-dot" aria-hidden="true" />
        <span className="conn-label">{CONN_LABEL[connState]}</span>
      </span>
      {/* Network / graph view toggle — routes to the Graph screen (React
          Router). Mirrors the pre-cutover #btn-graph markup/label/position
          (templates/layout.html), swapping its htmx-era click-to-mount
          behavior for a plain route Link. */}
      <Link
        id="btn-graph"
        className="theme-toggle"
        to="/graph"
        aria-label="Network view — toggle full graph"
        title="Network view"
      >
        {/* Nodes-and-edges network glyph */}
        <svg
          width="17"
          height="17"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.9"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <circle cx="6" cy="6" r="2.5" />
          <circle cx="18" cy="6" r="2.5" />
          <circle cx="6" cy="18" r="2.5" />
          <circle cx="18" cy="18" r="2.5" />
          <circle cx="12" cy="12" r="2.5" />
          <line x1="8" y1="6" x2="15.5" y2="6" />
          <line x1="6" y1="8" x2="6" y2="15.5" />
          <line x1="8" y1="18" x2="15.5" y2="18" />
          <line x1="18" y1="8" x2="18" y2="15.5" />
          <line x1="7.7" y1="7.7" x2="10.2" y2="10.2" />
          <line x1="16.3" y1="7.7" x2="13.8" y2="10.2" />
          <line x1="7.7" y1="16.3" x2="10.2" y2="13.8" />
          <line x1="16.3" y1="16.3" x2="13.8" y2="13.8" />
        </svg>
      </Link>
      <button
        type="button"
        className="theme-toggle"
        aria-label="Toggle light / dark theme"
        aria-pressed={theme === "dark"}
        onClick={toggle}
      >
        {theme === "dark" ? "☀" : "☾"}
      </button>
    </header>
  );
}
