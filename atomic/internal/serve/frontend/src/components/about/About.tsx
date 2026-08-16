// About — what this server is, and whether it is healthy.
//
// Folds the /status page's realm-health readout into a modal reachable from
// the icon rail, alongside the build identity a bug report needs first
// (version, commit, uptime) and the project's own links. Bus rooms come from
// the loopback-only /api/bus surface, so they are shown when that surface
// answers and silently omitted when it refuses — a LAN viewer still gets a
// complete About panel, minus a section that was never theirs to see.
import { Dialog } from "@ark-ui/react";
import {
  faCircleInfo,
  faCodeBranch,
  faBug,
  faArrowUpRightFromSquare,
} from "@fortawesome/free-solid-svg-icons";
import { useApi } from "../../utils/api";
import { FaGlyph } from "../ui";
import "./style.css";

const REPO_URL = "https://github.com/damusix/atomic-claude";
const ISSUES_URL = "https://github.com/damusix/atomic-claude/issues/new";
const AUTHOR_URL = "https://alonso.network";

interface StatusResponse {
  runId: string;
  version: string;
  commit: string;
  latestVersion: string;
  updateAvailable: boolean;
  uptimeSeconds: number;
  isRealmScope: boolean;
  wiki: {
    staleRepos: string[];
    staleConcerns: string[];
    staleBuckets: string[];
    bucketDiffKeys: string[];
    allFresh: boolean;
  };
  index: {
    severity: string;
    detail: string;
    freshCount: number;
    staleMembers: string[];
    notIndexed: string[];
  };
}

interface NavResponse {
  scope: string;
  name: string;
  branch?: string;
}

interface RoomInfo {
  name: string;
  members?: number;
  halted?: boolean;
}

interface RoomsResponse {
  running: boolean;
  rooms: RoomInfo[];
}

// Coarse on purpose: a server up for three days does not need its seconds.
function formatUptime(total: number): string {
  if (!Number.isFinite(total) || total < 0) return "—";
  const d = Math.floor(total / 86400);
  const h = Math.floor((total % 86400) / 3600);
  const m = Math.floor((total % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m`;
  return `${Math.floor(total)}s`;
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="about-row">
      <span className="about-row-label">{label}</span>
      <span className="about-row-value">{children}</span>
    </div>
  );
}

// Health reads as a list of what is wrong, and says so plainly when nothing
// is — a wall of green "OK" rows is noise the reader has to scan past.
function Health({ status }: { status: StatusResponse }) {
  const { wiki, index } = status;
  const problems: { label: string; items: string[] }[] = [
    { label: "stale repos", items: wiki.staleRepos },
    { label: "stale concerns", items: wiki.staleConcerns },
    { label: "stale buckets", items: wiki.staleBuckets },
    { label: "stale index", items: index.staleMembers },
    { label: "not indexed", items: index.notIndexed },
  ].filter((p) => p.items.length > 0);

  if (!problems.length) {
    return (
      <p className="about-health-clear">
        <span className="about-dot" data-ok /> Wiki and code index are current.
      </p>
    );
  }

  return (
    <div className="about-health">
      {problems.map((p) => (
        <div className="about-row" key={p.label}>
          <span className="about-row-label">{p.label}</span>
          <span className="about-pills">
            {p.items.map((item) => (
              <span className="about-pill" data-warn key={item}>
                {item}
              </span>
            ))}
          </span>
        </div>
      ))}
    </div>
  );
}

export function About({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { get } = useApi();
  // Ark keeps the content unmounted until open, so these only fire when the
  // panel is actually being read — a status probe per page load would be a
  // cost paid by everyone for a panel most never open.
  const { data: status } = get<StatusResponse>("/status");
  const { data: nav } = get<NavResponse>("/nav");
  const { data: rooms } = get<RoomsResponse>("/bus/rooms");

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(details) => {
        if (!details.open) onClose();
      }}
      lazyMount
      unmountOnExit
      aria-label="About this server"
    >
      <Dialog.Positioner id="about-modal" className={open ? "open" : ""}>
        <Dialog.Content className="about-box">
          <div className="about-header">
            <Dialog.Title className="about-title">
              <FaGlyph icon={faCircleInfo} size={14} />
              {nav?.name ?? "atomic serve"}
              {nav ? <span className="about-kind">{nav.scope === "realm" ? "realm" : "repo"}</span> : null}
            </Dialog.Title>
            <Dialog.CloseTrigger className="about-close" aria-label="Close">
              ✕
            </Dialog.CloseTrigger>
          </div>

          <div className="about-body">
            <section className="about-section">
              <Row label="version">
                <code>{status?.version ?? "…"}</code>
                {status?.commit && status.commit !== "unknown" ? (
                  <code className="about-commit">{status.commit.slice(0, 7)}</code>
                ) : null}
              </Row>
              {status?.latestVersion ? (
                <Row label="latest">
                  <code>{status.latestVersion}</code>
                  {status.updateAvailable ? (
                    <span className="about-update-tag">update available</span>
                  ) : null}
                </Row>
              ) : null}
              <Row label="uptime">{status ? formatUptime(status.uptimeSeconds) : "…"}</Row>
              {nav?.branch ? <Row label="branch">{nav.branch}</Row> : null}
            </section>

            {rooms?.running && rooms.rooms.length ? (
              <section className="about-section">
                <h3 className="about-section-title">Bus rooms</h3>
                <span className="about-pills">
                  {rooms.rooms.map((room) => (
                    <span className="about-pill" data-halted={room.halted || undefined} key={room.name}>
                      {room.name}
                      <span className="about-pill-count">{room.members ?? 0}</span>
                    </span>
                  ))}
                </span>
              </section>
            ) : null}

            <section className="about-section">
              <h3 className="about-section-title">Health</h3>
              {status ? <Health status={status} /> : <p className="about-health-clear">…</p>}
            </section>
          </div>

          <div className="about-footer">
            <span className="about-credit">
              Built by{" "}
              <a href={AUTHOR_URL} target="_blank" rel="noreferrer">
                Danilo Alonso
                <FaGlyph icon={faArrowUpRightFromSquare} size={9} />
              </a>{" "}
              · MIT
            </span>
            <span className="about-links">
              <a href={REPO_URL} target="_blank" rel="noreferrer">
                <FaGlyph icon={faCodeBranch} size={11} /> repo
              </a>
              <a href={ISSUES_URL} target="_blank" rel="noreferrer">
                <FaGlyph icon={faBug} size={11} /> report issue
              </a>
            </span>
          </div>
        </Dialog.Content>
      </Dialog.Positioner>
    </Dialog.Root>
  );
}
