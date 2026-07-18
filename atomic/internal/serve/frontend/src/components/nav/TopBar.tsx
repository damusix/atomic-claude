// TopBar — brand, breadcrumb, search trigger, connection indicator, theme
// toggle. Visual parity target: templates/layout.html's #app-header (pre-
// cutover markup, still the source of truth for structure/classes until the
// cutover checkpoint deletes it).
import { Link, useLocation } from "react-router";
import { useTheme } from "../../hooks/useTheme";
import "./style.css";

export type ConnState = "live" | "reconnecting" | "disconnected";

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
