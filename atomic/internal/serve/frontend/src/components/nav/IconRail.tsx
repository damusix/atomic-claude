// IconRail — the permanent slim mode rail. Browse toggles the nav drawer;
// the four view modes route below it; theme sits at the bottom. The modes and
// theme moved here from the top bar so the header carries only identity,
// scope, and search — the rail is the one place a mode is switched.
import { useState } from "react";
import { Link, useLocation } from "react-router";
import {
  faBars,
  faBookOpen,
  faDiagramProject,
  faDatabase,
  faComments,
  faCircleInfo,
  faSun,
  faMoon,
} from "@fortawesome/free-solid-svg-icons";
import type { IconDefinition } from "@fortawesome/free-solid-svg-icons";
import { useCapabilities } from "../../hooks/useCapabilities";
import { useTheme } from "../../hooks/useTheme";
import { useApi } from "../../utils/api";
import { About } from "../about/About";
import { FaGlyph, Tooltip } from "../ui";

interface Mode {
  to: string;
  icon: IconDefinition;
  label: string;
  /** Active when the current pathname matches — modes own more than one route. */
  match: (pathname: string) => boolean;
  /** Named capability this mode requires; absent means always shown. */
  requires?: "schema";
}

const MODES: Mode[] = [
  {
    to: "/",
    icon: faBookOpen,
    label: "Docs",
    match: (p) => p === "/" || p.startsWith("/page/"),
  },
  {
    to: "/graph",
    icon: faDiagramProject,
    label: "Graph",
    match: (p) => p === "/graph",
  },
  {
    to: "/code/schema",
    icon: faDatabase,
    label: "Schema",
    match: (p) => p.startsWith("/code"),
    requires: "schema",
  },
  {
    to: "/bus",
    icon: faComments,
    label: "Message Bus",
    match: (p) => p === "/bus",
  },
];

export function IconRail({ navOpen, onToggleNav }: { navOpen: boolean; onToggleNav: () => void }) {
  const { theme, toggle } = useTheme();
  const { pathname } = useLocation();
  const caps = useCapabilities();
  const [aboutOpen, setAboutOpen] = useState(false);
  // Read from the state file the background update check maintains; serve
  // never performs the check itself. The rail lives in the persistent shell,
  // so this is one request per page load, not one per navigation.
  const { data: status } = useApi().get<{ updateAvailable?: boolean }>("/status");
  const updateAvailable = status?.updateAvailable === true;

  // A mode already being viewed stays in the rail whatever the probe says —
  // removing the icon for the page someone is on would strand them without a
  // way back to it.
  const modes = MODES.filter(
    (m) => !m.requires || caps[m.requires] || m.match(pathname),
  );

  return (
    <aside className="icon-rail" aria-label="Modes">
      <Tooltip label={navOpen ? "Close Browse — Esc" : "Browse"}>
        <button
          type="button"
          className="icon-rail-btn"
          data-on={navOpen || undefined}
          aria-pressed={navOpen}
          aria-label="Browse — toggle navigation"
          onClick={onToggleNav}
        >
          <FaGlyph icon={faBars} size={15} />
        </button>
      </Tooltip>

      <div className="icon-rail-divider" />

      {modes.map((mode) => {
        const active = mode.match(pathname);
        return (
          <Tooltip key={mode.to} label={mode.label}>
            <Link
              to={mode.to}
              className="icon-rail-btn"
              data-on={active || undefined}
              aria-current={active ? "page" : undefined}
              aria-label={mode.label}
            >
              <FaGlyph icon={mode.icon} size={15} />
            </Link>
          </Tooltip>
        );
      })}

      <Tooltip label={theme === "dark" ? "Light theme" : "Dark theme"}>
        <button
          type="button"
          className="icon-rail-btn icon-rail-bottom"
          aria-label="Toggle light / dark theme"
          aria-pressed={theme === "dark"}
          onClick={toggle}
        >
          <FaGlyph icon={theme === "dark" ? faSun : faMoon} size={14} />
        </button>
      </Tooltip>

      <Tooltip label={updateAvailable ? "About & status — update available" : "About & status"}>
        <button
          type="button"
          className="icon-rail-btn"
          aria-label={updateAvailable ? "About this server — update available" : "About this server"}
          aria-haspopup="dialog"
          onClick={() => setAboutOpen(true)}
        >
          <FaGlyph icon={faCircleInfo} size={14} />
          {/* A dot, not a count: the only thing to say is that a newer
              release exists, and the version numbers are one click away
              inside the panel. */}
          {updateAvailable ? <span className="icon-rail-badge" aria-hidden="true" /> : null}
        </button>
      </Tooltip>

      <About open={aboutOpen} onClose={() => setAboutOpen(false)} />
    </aside>
  );
}
