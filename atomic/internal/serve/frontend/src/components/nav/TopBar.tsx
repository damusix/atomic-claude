// TopBar — brand, breadcrumb, search trigger, connection indicator. View
// modes and the theme toggle live in components/nav/IconRail: a mode is
// switched in exactly one place, and the header stays a statement of where
// you are rather than a second control cluster.
import { Fragment } from "react";
import { Link, useLocation } from "react-router";
import type { ConnState } from "../../hooks/useLiveReload";
import { useApi } from "../../utils/api";
import { Tooltip } from "../ui";
import type { NavResponse } from "./types";
import "./style.css";

export type { ConnState };

const CONN_LABEL: Record<ConnState, string> = {
  live: "live",
  reconnecting: "connecting…",
  disconnected: "disconnected",
};

// The scope chip names what is being served and which kind of thing it is.
// It used to read a hardcoded "atomic" beside the equally hardcoded brand,
// so the header showed the product name twice and the realm/repo never.
function ScopeChip() {
  const { get } = useApi();
  const { data } = get<NavResponse>("/nav");
  if (!data) return null;

  const kind = data.scope === "realm" ? "realm" : "repo";
  const label = data.branch
    ? `${kind} scope — ${data.name}, on branch ${data.branch}`
    : `${kind} scope — ${data.name}`;

  return (
    <Tooltip label={label} placement="bottom">
      <span className="breadcrumb-scope" data-scope={kind}>
        <span className="breadcrumb-scope-main">
          {data.name}
          <span className="breadcrumb-scope-kind">{kind}</span>
        </span>
        {data.branch ? <span className="breadcrumb-scope-branch">{data.branch}</span> : null}
      </span>
    </Tooltip>
  );
}

// The header breadcrumb shows the whole path, not just the leaf: with the
// library behind a drawer, this is the only always-visible answer to "where
// am I in the tree".
function Breadcrumb() {
  const location = useLocation();
  const segments = location.pathname.replace(/^\/page\//, "").split("/").filter(Boolean);

  return (
    <nav className="breadcrumb" aria-label="Breadcrumb">
      <ScopeChip />
      {segments.length === 0 ? (
        <>
          <span className="breadcrumb-sep">›</span>
          <span className="breadcrumb-page">home</span>
        </>
      ) : (
        // Fragments, not wrapper spans: .breadcrumb is a flex row whose gap
        // supplies the spacing, and a wrapper would collapse each separator
        // against its own label.
        // Every segment but the last navigates to that directory's listing —
        // this is the page's only breadcrumb, so it has to actually work.
        segments.map((segment, i) => {
          const last = i === segments.length - 1;
          const path = segments.slice(0, i + 1).join("/");
          return (
            <Fragment key={`${segment}:${i}`}>
              <span className="breadcrumb-sep">›</span>
              {last ? (
                <span className="breadcrumb-page">{segment}</span>
              ) : (
                <Link className="breadcrumb-folder" to={`/page/${path}`}>
                  {segment}
                </Link>
              )}
            </Fragment>
          );
        })
      )}
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
  return (
    <header id="app-header">
      {/* The mark alone: the wordmark beside it was the same brand said twice.
          data-conn drives the glow, so the header carries the same live/idle
          signal the favicon does. */}
      <Link className="brand" to="/" aria-label="atomic — home" data-conn={connState}>
        <img className="brand-logo" src="/logo.png" alt="atomic" />
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
    </header>
  );
}
