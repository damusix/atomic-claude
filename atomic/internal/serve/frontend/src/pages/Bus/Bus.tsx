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

// mentionQuery reports the @fragment currently being typed, or null when
// the mention dropdown should be closed. Addressing is chips-first: a
// mention is only ever typed at the start of an empty-body draft, so the
// dropdown is active exactly while the draft is one leading @token — an @
// in the body never pops it.
export function mentionQuery(draft: string): string | null {
  const m = draft.match(/^@(\S*)$/);
  return m ? m[1] : null;
}

// chipCommit detects a mention completed by typing a space ("@fable " →
// commit "fable" as a chip, keep the rest as the draft). Null when the
// draft is not a freshly space-terminated leading mention. [\s\S] because
// the composer is a textarea — the rest may span lines.
export function chipCommit(draft: string): { chip: string; rest: string } | null {
  const m = draft.match(/^@(\S+)\s+([\s\S]*)$/);
  return m ? { chip: m[1], rest: m[2] } : null;
}

// resolveMember mirrors the daemon's --to resolution for the addressee
// chips: exact name first, else a unique substring; anything ambiguous or
// unmatched resolves to null (the chip renders the raw fragment instead).
export function resolveMember(names: string[], frag: string): string | null {
  if (names.includes(frag)) return frag;
  const matches = names.filter((n) => n.includes(frag));
  return matches.length === 1 ? matches[0] : null;
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
  const [note, setNote] = useState<string | null>(null);
  const [closed, setClosed] = useState(false);
  const [following, setFollowing] = useState(true);
  const seenIds = useRef(new Set<string>());
  const scrollRef = useRef<HTMLDivElement | null>(null);
  // followingRef mirrors `following` for the append effect below — the
  // decision to stick must come from where the operator was *before* the
  // new message rendered, not from re-measuring after it grew the scroll
  // height (a tall message would silently kick the view out of follow).
  const followingRef = useRef(true);

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

  // Follow the transcript while the operator is at the bottom; scrolling
  // up parks the view and new traffic stops moving it (the jump button
  // below is the way back).
  useEffect(() => {
    const el = scrollRef.current;
    if (el && followingRef.current) el.scrollTop = el.scrollHeight;
  }, [envelopes]);

  // The composer growing shrinks the transcript pane — re-pin to the
  // bottom on any size change while following, so typing a long message
  // doesn't shove the latest traffic out of view.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      if (followingRef.current) el.scrollTop = el.scrollHeight;
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  function onScroll() {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    followingRef.current = atBottom;
    setFollowing(atBottom);
  }

  function jumpToBottom() {
    const el = scrollRef.current;
    if (!el) return;
    followingRef.current = true;
    setFollowing(true);
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
  }

  async function sendMessage(to: string[], text: string): Promise<boolean> {
    if (!text.trim()) return false;
    setNote(null);
    const [res, err] = await attempt(() =>
      api.post<BusSendResponse>("/bus/send", { room, text, to }),
    );
    if (err) {
      setNote("send failed — bus unreachable");
      return false;
    }
    if (!res.ok) {
      setNote(`send failed (${res.status})`);
      return false;
    }
    if (res.data) {
      appendEnvelopes([res.data.envelope]);
      if (res.data.unknown_to?.length) {
        setNote(`no member matching @${res.data.unknown_to.join(", @")} — delivered room-wide`);
      }
    }
    onRosterChange();
    return true;
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

      <div className="bus-transcript-wrap">
        <div className="bus-transcript" ref={scrollRef} onScroll={onScroll}>
          {envelopes.map((env) => (
          <article
            key={env.id}
            className={env.from === selfName ? "bus-msg bus-msg-self" : "bus-msg"}
            data-kind={env.from_kind}
          >
            <div className="bus-msg-meta">
              <span className="bus-msg-sender">
                <span className="bus-msg-from">{env.from}</span>
                <span className="bus-msg-kindpill" data-kind={env.from_kind}>
                  {env.from_kind}
                </span>
              </span>
              {env.to.length > 0 ? (
                <span className="bus-msg-to">
                  <span className="bus-msg-to-label">to</span> {env.to.join(", ")}
                </span>
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
        {!following ? (
          <button type="button" className="bus-jump-bottom" onClick={jumpToBottom} aria-label="Scroll to latest">
            ↓ latest
          </button>
        ) : null}
      </div>

      {note ? <p className="bus-note">{note}</p> : null}

      <Composer
        members={(who?.members ?? []).map((m) => m.name).filter((n) => n !== selfName)}
        disabled={closed}
        onSend={sendMessage}
      />
    </section>
  );
}

// Composer — a chips-first message input. Typing @ at the start of the
// draft opens a dropdown of room members (click / ArrowUp/Down / Enter /
// Tab select, Escape dismisses); a picked or space-completed mention
// becomes a removable addressee chip on the left of the input and leaves
// the text. Backspace on an empty draft pops the last chip. Send delivers
// chips as the to-list and the draft as the body.
function Composer({
  members,
  disabled,
  onSend,
}: {
  members: string[];
  disabled: boolean;
  onSend: (to: string[], text: string) => Promise<boolean>;
}) {
  const [chips, setChips] = useState<string[]>([]);
  const [draft, setDraft] = useState("");
  const [highlight, setHighlight] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const taRef = useRef<HTMLTextAreaElement | null>(null);

  // Auto-grow the textarea with its content, capped by the CSS max-height
  // — long messages wrap into new lines instead of scrolling horizontally.
  useEffect(() => {
    const ta = taRef.current;
    if (!ta) return;
    ta.style.height = "auto";
    ta.style.height = `${ta.scrollHeight}px`;
  }, [draft]);

  const frag = mentionQuery(draft);
  const suggestions =
    frag === null || dismissed
      ? []
      : members.filter(
          (n) => !chips.includes(n) && n.toLowerCase().includes(frag.toLowerCase()),
        );
  const active = Math.min(highlight, Math.max(0, suggestions.length - 1));

  function addChip(name: string) {
    setChips((prev) => (prev.includes(name) ? prev : [...prev, name]));
    setDraft("");
    setHighlight(0);
  }

  function update(next: string) {
    setDismissed(false);
    setHighlight(0);
    // A space right after a leading mention commits it as a chip — typing
    // "@fable hello" addresses fable without ever touching the dropdown.
    const commit = chipCommit(next);
    if (commit) {
      setChips((prev) => (prev.includes(commit.chip) ? prev : [...prev, commit.chip]));
      setDraft(commit.rest);
      return;
    }
    setDraft(next);
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Backspace" && draft === "" && chips.length > 0) {
      setChips((prev) => prev.slice(0, -1));
      return;
    }
    if (suggestions.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setHighlight((active + 1) % suggestions.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setHighlight((active - 1 + suggestions.length) % suggestions.length);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        addChip(suggestions[active]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setDismissed(true);
        return;
      }
    }
    // Enter sends; Shift+Enter makes a newline — the textarea's default.
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void submit();
    }
  }

  async function submit() {
    if (await onSend(chips, draft)) {
      setChips([]);
      setDraft("");
    }
  }

  return (
    <form
      className="bus-composer"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <div className="bus-composer-field">
        {chips.map((c) => {
          const resolved = resolveMember(members, c);
          return (
            <button
              type="button"
              key={c}
              className={resolved ? "bus-chip" : "bus-chip bus-chip-unknown"}
              title="Remove addressee"
              onClick={() => setChips((prev) => prev.filter((x) => x !== c))}
            >
              <span className="bus-chip-label">to</span> {resolved ?? c}
              <span className="bus-chip-x" aria-hidden="true">
                ×
              </span>
            </button>
          );
        })}
        <textarea
          ref={taRef}
          rows={1}
          value={draft}
          onChange={(e) => update(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder={chips.length > 0 ? "message… (Enter sends, Shift+Enter newline)" : "@ to address someone, plain text for room-wide FYI…"}
          aria-label="Message"
          disabled={disabled}
        />
        {suggestions.length > 0 ? (
          <ul className="bus-mention-list" role="listbox" aria-label="Room members">
            {suggestions.map((n, i) => (
              <li key={n}>
                <button
                  type="button"
                  role="option"
                  aria-selected={i === active}
                  className={i === active ? "bus-mention-item bus-mention-active" : "bus-mention-item"}
                  onMouseDown={(e) => {
                    e.preventDefault();
                    addChip(n);
                  }}
                >
                  @{n}
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
      <button type="submit" disabled={disabled}>
        Send
      </button>
    </form>
  );
}
