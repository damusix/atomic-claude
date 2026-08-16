// useReindex — start and follow an index rebuild for one member.
//
// The rebuild runs detached on the server (it outlives any sane request
// timeout), so this starts it with a POST and then polls the same endpoint
// for completion. Polling stops as soon as the job leaves `running`, so an
// idle page issues no traffic.
import { useCallback, useEffect, useRef, useState } from "react";
import { attempt } from "@logosdx/utils";
import { api } from "../utils/api";

export type ReindexState = "idle" | "running" | "done" | "failed";

interface ReindexJob {
  state: ReindexState;
  error?: string;
}

const POLL_MS = 1200;

export function useReindex(member: string, onComplete?: () => void) {
  const [state, setState] = useState<ReindexState>("idle");
  const [error, setError] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);
  // Held in a ref so the poll callback always calls the current one without
  // the interval having to be torn down and rebuilt on every render.
  const completeRef = useRef(onComplete);
  completeRef.current = onComplete;

  const query = member ? `?member=${encodeURIComponent(member)}` : "";

  const stopPolling = useCallback(() => {
    if (timer.current) clearInterval(timer.current);
    timer.current = null;
  }, []);

  const readJob = useCallback(async () => {
    const [res, err] = await attempt(() => api.get<ReindexJob>(`/code/index${query}`));
    if (err || !res?.ok || !res.data) return null;
    return res.data;
  }, [query]);

  // A rebuild started in another tab (or before this page mounted) is still
  // this member's rebuild — adopt it rather than reporting idle over a job
  // that is actually running.
  useEffect(() => {
    let cancelled = false;
    void readJob().then((job) => {
      if (cancelled || !job) return;
      setState(job.state);
      if (job.state === "running") startPolling();
    });
    return () => {
      cancelled = true;
      stopPolling();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- startPolling is stable for a given member
  }, [query]);

  function startPolling() {
    stopPolling();
    timer.current = setInterval(() => {
      void readJob().then((job) => {
        if (!job) return;
        setState(job.state);
        setError(job.error ?? null);
        if (job.state !== "running") {
          stopPolling();
          if (job.state === "done") completeRef.current?.();
        }
      });
    }, POLL_MS);
  }

  const start = useCallback(async () => {
    setError(null);
    setState("running");
    const [res, err] = await attempt(() => api.post<ReindexJob>(`/code/index${query}`, {}));
    if (err || !res?.ok) {
      setState("failed");
      // The endpoint is loopback-only, which is the failure a remote viewer
      // will actually hit — say so rather than reporting a bare error.
      setError(res?.status === 403 ? "Reindex is only available on the serving machine." : "Could not start the rebuild.");
      return;
    }
    startPolling();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- startPolling is stable for a given member
  }, [query]);

  return { state, error, start };
}
