// useSearchStream — consumes GET /api/search/stream?q=&src= (named SSE
// events md/code/end, api_handlers.go NewAPISearchStreamHandler). EventSource
// is exempt from the shared-FetchEngine rule (frontend/CLAUDE.md) — it is not
// fetch-based and has no resilience config to hand off to.
import { useEffect, useState } from "react";
import type { ApiMdSearchResponse, ApiSearchStreamCodeEvent, SearchSrc } from "./types";

export interface SearchStreamState {
  md: ApiMdSearchResponse | null;
  code: ApiSearchStreamCodeEvent[];
  done: boolean;
}

const IDLE_STATE: SearchStreamState = { md: null, code: [], done: true };

export function useSearchStream(query: string, src: SearchSrc): SearchStreamState {
  const [state, setState] = useState<SearchStreamState>(IDLE_STATE);

  useEffect(() => {
    if (!query) {
      setState(IDLE_STATE);
      return;
    }

    setState({ md: null, code: [], done: false });

    const url = `${window.location.origin}/api/search/stream?q=${encodeURIComponent(query)}&src=${src}`;
    const source = new EventSource(url);

    source.addEventListener("md", (e) => {
      const data = JSON.parse((e as MessageEvent).data) as ApiMdSearchResponse;
      setState((s) => ({ ...s, md: data }));
    });
    source.addEventListener("code", (e) => {
      const data = JSON.parse((e as MessageEvent).data) as ApiSearchStreamCodeEvent;
      setState((s) => ({ ...s, code: [...s.code, data] }));
    });
    source.addEventListener("end", () => {
      setState((s) => ({ ...s, done: true }));
      source.close();
    });
    source.onerror = () => {
      setState((s) => ({ ...s, done: true }));
      source.close();
    };

    return () => source.close();
  }, [query, src]);

  return state;
}
