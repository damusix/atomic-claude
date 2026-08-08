// BusRail — right-rail content for the /bus route (EXPERIMENT, bus chat):
// the sessions behind each room member, clickable to open that session's
// .jsonl transcript rendered as markdown (BusTranscriptModal).
import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { attempt } from "@logosdx/utils";
import { api } from "../../utils/api";
import { BusTranscriptModal } from "./BusTranscriptModal";

interface BusSessionTranscript {
  found: boolean;
  path?: string;
  mtime?: number;
  size?: number;
}

export interface BusSessionInfo {
  name: string;
  kind: string;
  session: string;
  stale: boolean;
  repo?: string;
  realm?: string;
  transcript: BusSessionTranscript;
}

interface BusSessionsResponse {
  sessions: BusSessionInfo[];
}

function formatAge(mtimeUnix: number): string {
  const seconds = Math.max(0, Math.floor(Date.now() / 1000 - mtimeUnix));
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

export function BusRail() {
  const [params] = useSearchParams();
  const room = params.get("room") ?? "";
  const [sessions, setSessions] = useState<BusSessionInfo[]>([]);
  const [open, setOpen] = useState<BusSessionInfo | null>(null);

  const refresh = useCallback(() => {
    if (!room) {
      setSessions([]);
      return;
    }
    void attempt(() =>
      api.get<BusSessionsResponse>(`/bus/sessions?room=${encodeURIComponent(room)}`),
    ).then(([res, err]) => {
      if (!err && res?.ok && res.data) setSessions(res.data.sessions);
    });
  }, [room]);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 7000);
    return () => clearInterval(id);
  }, [refresh]);

  if (!room) {
    return null;
  }

  return (
    <section className="rail-slot bus-rail">
      <h4 className="bus-rail-title">Sessions</h4>
      <ul className="bus-rail-list">
        {sessions.map((s) => (
          <li key={s.session} className="bus-rail-item" data-kind={s.kind}>
            <div className="bus-rail-member">
              <span className="bus-rail-name">{s.name}</span>
              <span className="bus-rail-kind">{s.kind}</span>
              {s.stale ? <span className="bus-rail-stale">stale</span> : null}
            </div>
            {s.transcript.found ? (
              <button
                type="button"
                className="bus-rail-session bus-rail-session-link"
                title={s.transcript.path}
                onClick={() => setOpen(s)}
              >
                <span className="bus-rail-session-id">{s.session}</span>
                {s.transcript.mtime ? (
                  <span className="bus-rail-session-age">{formatAge(s.transcript.mtime)}</span>
                ) : null}
              </button>
            ) : (
              <span className="bus-rail-session" title="no transcript found under ~/.claude/projects">
                <span className="bus-rail-session-id">{s.session}</span>
                <span className="bus-rail-session-age">no transcript</span>
              </span>
            )}
          </li>
        ))}
        {sessions.length === 0 ? <li className="bus-rail-empty">no members</li> : null}
      </ul>
      {open ? <BusTranscriptModal member={open} onClose={() => setOpen(null)} /> : null}
    </section>
  );
}
