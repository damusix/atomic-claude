// useLiveReload — ports templates/layout.html's live-reload EventSource
// boot (spec Flow: live-reload reconcile). Opens EventSource('/events'),
// tracks the connectivity indicator state, and emits "realm.changed" on the
// shared observer bus (utils/events) for the dedup'd, non-seed messages —
// consumers (nav: unconditional refetch; page/rail: conditional on
// `shouldRefetchPage`) subscribe independently; this hook has no opinion on
// who reacts.
//
// EventSource is exempt from the shared-FetchEngine rule (frontend/CLAUDE.md,
// see also components/search/useSearchStream.ts) — it is not fetch-based and
// has no resilience config to hand off to.
//
// Cache-bypass caution (spec's conventions section): the shared FetchEngine
// (utils/api.ts) sets no `cachePolicy` — every refetch this hook triggers
// (nav/page/rail, via consumers subscribing to "realm.changed") hits the
// network. Nothing to invalidate; this is a verified no-op, not an oversight.
import { useEffect, useState } from "react";
import { events } from "../utils/events";

export type ConnState = "live" | "reconnecting" | "disconnected";

interface ChangeEvent {
  fp: string;
  changed?: string[];
}

// shouldRefetchPage decides whether the currently open page/rail must
// refetch given a realm.changed payload: an omitted `changed` list means the
// server's diff exceeded its cap (changedCap in events.go) and everything is
// treated as changed; otherwise refetch only when the open relpath is in the
// list. No open page (relpath === null — directory listing, 404, or nothing
// mounted yet) never refetches.
export function shouldRefetchPage(relpath: string | null, changed?: string[]): boolean {
  if (!relpath) return false;
  if (!changed) return true;
  return changed.includes(relpath);
}

export function useLiveReload(): { connState: ConnState } {
  const [connState, setConnState] = useState<ConnState>("reconnecting");

  useEffect(() => {
    if (typeof EventSource === "undefined") return;

    let lastSeenFp: string | null = null;
    const source = new EventSource("/events");

    source.onopen = () => setConnState("live");

    // onerror fires both for a genuine terminal failure and for the
    // browser's built-in retry attempts; readyState tells them apart
    // (CLOSED only after the browser gives up).
    source.onerror = () => {
      setConnState(source.readyState === EventSource.CLOSED ? "disconnected" : "reconnecting");
    };

    source.onmessage = (msgEvt) => {
      let ev: ChangeEvent;
      try {
        ev = JSON.parse(msgEvt.data);
      } catch {
        return;
      }
      // The resync push is always the first message a connection receives,
      // sent unconditionally regardless of whether anything changed since
      // the page was rendered — seed lastSeenFp without dispatching, or
      // every fresh page load would spuriously refetch itself.
      if (lastSeenFp === null) {
        lastSeenFp = ev.fp;
        return;
      }
      if (ev.fp === lastSeenFp) return;
      events.emit("realm.changed", ev);
      lastSeenFp = ev.fp;
    };

    return () => source.close();
  }, []);

  return { connState };
}
