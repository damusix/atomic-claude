// useGraphWarm — kicks off the background graph layout once per server run,
// and gets out of the way the moment the reader opens the graph for real.
//
// Keyed on the server's run id (/api/status) rather than a timestamp: a
// restart may be serving different content, so a layout warmed by a previous
// process is not assumed to still apply.
import { useEffect } from "react";
import { useLocation } from "react-router";
import { attempt } from "@logosdx/utils";
import { api } from "../utils/api";
import { cancelWarm, warmDocsGraph } from "../utils/graphWarm";

interface StatusResponse {
  runId?: string;
}

/** Delay before warming, so the page the reader actually asked for renders
    first. The warm is a background convenience and must never be what makes
    the first paint late. */
const WARM_DELAY_MS = 2000;

export function useGraphWarm(): void {
  const { pathname } = useLocation();
  const onGraph = pathname === "/graph";

  // Opening the graph takes the engine over: it holds one instance, so a warm
  // still running would be destroyed by the real mount anyway. Cancelling
  // explicitly releases the offscreen WebGL context instead of orphaning it.
  useEffect(() => {
    if (onGraph) cancelWarm();
  }, [onGraph]);

  useEffect(() => {
    if (onGraph) return;

    let cancelled = false;
    const timer = setTimeout(() => {
      void attempt(() => api.get<StatusResponse>("/status")).then(([res, err]) => {
        if (cancelled || err || !res?.ok) return;
        const runId = res.data?.runId;
        if (!runId) return;
        void warmDocsGraph(runId);
      });
    }, WARM_DELAY_MS);

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
    // Runs once per mount, not per navigation: warmDocsGraph is itself
    // idempotent for a given run id, so re-firing on every route change would
    // only add no-op status requests.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}
