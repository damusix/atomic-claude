// Bus — /bus page: EXPERIMENT — web chat over the atomic bus daemon.
// Watch any room's traffic (log backfill + SSE tail), open a channel (join
// creates the room if absent), speak as the human operator with @fragment
// addressing, and halt/resume a room. Backed by /api/bus/* (api_bus.go).
//
// EventSource is exempt from the shared-FetchEngine rule (see
// hooks/useLiveReload.ts) — it is not fetch-based.
import { useCallback, useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { attempt } from "@logosdx/utils";
import { api } from "../../utils/api";
import "./style.css";

interface BusRoomInfo {
  name: string;
  members: number;
  halted?: boolean;
  halt_reason?: string;
}

interface BusRoomsResponse {
  running: boolean;
  rooms: BusRoomInfo[];
}

interface BusStatusResponse {
  running: boolean;
  name: string;
  repo?: string;
  realm?: string;
}

interface BusMember {
  name: string;
  kind: string;
  mode?: string;
  stale: boolean;
}

interface BusWhoResponse {
  halted: boolean;
  halt_reason?: string;
  members: BusMember[];
}

interface BusEnvelope {
  id: string;
  room: string;
  from: string;
  from_kind: string;
  from_repo?: string;
  from_realm?: string;
  to: string[];
  reply_to?: string;
  ts: number;
  text: string;
  closing?: boolean;
}

interface BusSendResponse {
  envelope: BusEnvelope;
  unknown_to?: string[];
  name: string;
}

interface BusLogResponse {
  envelopes: BusEnvelope[];
}

// parseComposer splits leading @fragment tokens off the message body:
// "@fe @be run the tests" → { to: ["fe", "be"], text: "run the tests" }.
// No leading @tokens → an FYI message (empty to).
export function parseComposer(input: string): { to: string[]; text: string } {
  const tokens = input.trim().split(/\s+/);
  const to: string[] = [];
  let i = 0;
  while (i < tokens.length && tokens[i].startsWith("@") && tokens[i].length > 1) {
    to.push(tokens[i].slice(1));
    i += 1;
  }
  return { to, text: tokens.slice(i).join(" ") };
}

function formatTime(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function Bus() {
  const [params, setParams] = useSearchParams();
  const room = params.get("room") ?? "";

  const [status, setStatus] = useState<BusStatusResponse | null>(null);
  const [rooms, setRooms] = useState<BusRoomInfo[]>([]);
  const [running, setRunning] = useState(false);
  const [newRoom, setNewRoom] = useState("");
  const [joinError, setJoinError] = useState<string | null>(null);

  const refreshRooms = useCallback(() => {
    void attempt(() => api.get<BusRoomsResponse>("/bus/rooms")).then(([res, err]) => {
      if (err || !res?.ok || !res.data) return;
      setRunning(res.data.running);
      setRooms(res.data.rooms);
    });
  }, []);

  useEffect(() => {
    void attempt(() => api.get<BusStatusResponse>("/bus/status")).then(([res, err]) => {
      if (!err && res?.ok && res.data) setStatus(res.data);
    });
  }, []);

  useEffect(() => {
    refreshRooms();
    const id = setInterval(refreshRooms, 4000);
    return () => clearInterval(id);
  }, [refreshRooms]);

  function openRoom(name: string) {
    setParams((prev) => {
      const p = new URLSearchParams(prev);
      p.set("room", name);
      return p;
    });
  }

  async function joinRoom(name: string) {
    setJoinError(null);
    const [res, err] = await attempt(() => api.post<{ name: string }>("/bus/join", { room: name }));
    if (err) {
      setJoinError("bus unreachable — is the daemon startable?");
      return;
    }
    if (!res.ok) {
      setJoinError(`join failed (${res.status})`);
      return;
    }
    setNewRoom("");
    refreshRooms();
    openRoom(name);
  }

  return (
    <div className="bus-page" data-route="bus">
      <aside className="bus-sidebar">
        <div className="bus-sidebar-head">
          <h2 className="bus-title">Bus</h2>
          <span className={running ? "bus-daemon bus-daemon-up" : "bus-daemon bus-daemon-down"}>
            {running ? "daemon up" : "daemon down"}
          </span>
        </div>
        {status?.name ? (
          <p className="bus-identity">
            you are <strong>{status.name}</strong>
          </p>
        ) : null}
        <ul className="bus-room-list">
          {rooms.map((r) => (
            <li key={r.name}>
              <button
                type="button"
                className={r.name === room ? "bus-room bus-room-active" : "bus-room"}
                onClick={() => openRoom(r.name)}
              >
                <span className="bus-room-name">{r.name}</span>
                <span className="bus-room-meta">
                  {r.halted ? <span className="bus-halt-chip">halted</span> : null}
                  {r.members}
                </span>
              </button>
            </li>
          ))}
          {rooms.length === 0 ? <li className="bus-room-empty">no rooms</li> : null}
        </ul>
        <form
          className="bus-new-room"
          onSubmit={(e) => {
            e.preventDefault();
            const name = newRoom.trim();
            if (name) void joinRoom(name);
          }}
        >
          <input
            type="text"
            value={newRoom}
            onChange={(e) => setNewRoom(e.target.value)}
            placeholder="open a channel…"
            aria-label="Room name to open"
          />
          <button type="submit">Join</button>
        </form>
        {joinError ? <p className="bus-error">{joinError}</p> : null}
        {!running ? <p className="bus-hint">Opening a channel starts the daemon.</p> : null}
      </aside>

      {room ? (
        <RoomView key={room} room={room} selfName={status?.name ?? ""} onRosterChange={refreshRooms} />
      ) : (
        <div className="bus-empty">
          <p>Pick a room, or open a channel to start one.</p>
          <p className="bus-hint">
            Agents join with <code>atomic bus join &lt;room&gt;</code>; everything they publish lands here live.
          </p>
        </div>
      )}
    </div>
  );
}

function RoomView({
  room,
  selfName,
  onRosterChange,
}: {
  room: string;
  selfName: string;
  onRosterChange: () => void;
}) {
  const [envelopes, setEnvelopes] = useState<BusEnvelope[]>([]);
  const [who, setWho] = useState<BusWhoResponse | null>(null);
  const [draft, setDraft] = useState("");
  const [note, setNote] = useState<string | null>(null);
  const [closed, setClosed] = useState(false);
  const seenIds = useRef(new Set<string>());
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const appendEnvelopes = useCallback((incoming: BusEnvelope[]) => {
    const fresh = incoming.filter((env) => env.id && !seenIds.current.has(env.id));
    if (fresh.length === 0) return;
    for (const env of fresh) seenIds.current.add(env.id);
    setEnvelopes((prev) => [...prev, ...fresh]);
  }, []);

  const refreshWho = useCallback(() => {
    void attempt(() => api.get<BusWhoResponse>(`/bus/who?room=${encodeURIComponent(room)}`)).then(
      ([res, err]) => {
        if (!err && res?.ok && res.data) setWho(res.data);
      },
    );
  }, [room]);

  // Backfill from the durable room log, then attach the live tail. The
  // tail sees everything published after it connects; ids dedupe the
  // overlap between the two.
  useEffect(() => {
    void attempt(() => api.get<BusLogResponse>(`/bus/log?room=${encodeURIComponent(room)}&n=200`)).then(
      ([res, err]) => {
        if (!err && res?.ok && res.data) appendEnvelopes(res.data.envelopes);
      },
    );
    refreshWho();
    const whoTimer = setInterval(refreshWho, 7000);

    if (typeof EventSource === "undefined") return () => clearInterval(whoTimer);
    const source = new EventSource(`/api/bus/tail?room=${encodeURIComponent(room)}`);
    source.onmessage = (msgEvt) => {
      let env: BusEnvelope;
      try {
        env = JSON.parse(msgEvt.data);
      } catch {
        return;
      }
      appendEnvelopes([env]);
      if (env.closing) {
        setClosed(true);
        source.close();
      }
    };
    return () => {
      clearInterval(whoTimer);
      source.close();
    };
  }, [room, appendEnvelopes, refreshWho]);

  // Follow the transcript unless the operator scrolled up to read history.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 120;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [envelopes]);

  async function sendDraft() {
    const { to, text } = parseComposer(draft);
    if (!text) return;
    setNote(null);
    const [res, err] = await attempt(() =>
      api.post<BusSendResponse>("/bus/send", { room, text, to }),
    );
    if (err) {
      setNote("send failed — bus unreachable");
      return;
    }
    if (!res.ok) {
      setNote(`send failed (${res.status})`);
      return;
    }
    if (res.data) {
      appendEnvelopes([res.data.envelope]);
      if (res.data.unknown_to?.length) {
        setNote(`no member matching @${res.data.unknown_to.join(", @")} — delivered room-wide`);
      }
    }
    setDraft("");
    onRosterChange();
  }

  async function setHalt(halt: boolean) {
    const path = halt ? "/bus/halt" : "/bus/resume";
    const body = halt ? { room, reason: "halted from serve" } : { room };
    await attempt(() => api.post(path, body));
    refreshWho();
    onRosterChange();
  }

  return (
    <section className="bus-room-view">
      <header className="bus-room-head">
        <h3 className="bus-room-title">{room}</h3>
        <div className="bus-members">
          {(who?.members ?? []).map((m) => (
            <span
              key={m.name}
              className={m.stale ? "bus-member bus-member-stale" : "bus-member"}
              data-kind={m.kind}
              title={`${m.kind}${m.mode ? ` · ${m.mode}` : ""}${m.stale ? " · stale" : ""}`}
            >
              {m.name}
            </span>
          ))}
        </div>
        <button type="button" className="bus-halt-btn" onClick={() => void setHalt(!who?.halted)}>
          {who?.halted ? "Resume" : "Halt"}
        </button>
      </header>

      {who?.halted ? (
        <div className="bus-halted-banner">
          room halted{who.halt_reason ? ` — ${who.halt_reason}` : ""}; agents cannot send, you still can
        </div>
      ) : null}
      {closed ? <div className="bus-halted-banner">room closed</div> : null}

      <div className="bus-transcript" ref={scrollRef}>
        {envelopes.map((env) => (
          <article
            key={env.id}
            className={env.from === selfName ? "bus-msg bus-msg-self" : "bus-msg"}
            data-kind={env.from_kind}
          >
            <div className="bus-msg-meta">
              <span className="bus-msg-from">{env.from}</span>
              <span className="bus-msg-kind">{env.from_kind}</span>
              {env.to.length > 0 ? (
                <span className="bus-msg-to">→ {env.to.join(", ")}</span>
              ) : (
                <span className="bus-msg-fyi">fyi</span>
              )}
              <span className="bus-msg-time">{formatTime(env.ts)}</span>
            </div>
            <div className="bus-msg-text">{env.text}</div>
          </article>
        ))}
        {envelopes.length === 0 ? <p className="bus-hint">No traffic yet.</p> : null}
      </div>

      {note ? <p className="bus-note">{note}</p> : null}

      <form
        className="bus-composer"
        onSubmit={(e) => {
          e.preventDefault();
          void sendDraft();
        }}
      >
        <input
          type="text"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="@name to address, plain text for room-wide FYI…"
          aria-label="Message"
          disabled={closed}
        />
        <button type="submit" disabled={closed}>
          Send
        </button>
      </form>
    </section>
  );
}
