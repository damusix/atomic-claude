// useCapabilities — what the shell should offer for the scope being served.
//
// Only the SQL schema view so far. Most repositories have no SQL, and a
// permanent mode that can only ever say "nothing here" is a promise the tool
// cannot keep, so the mode is absent unless the index holds SQL objects (or
// .claude/atomic.toml says otherwise). The server decides; this only asks.
import { useEffect, useState } from "react";
import { attempt } from "@logosdx/utils";
import { api } from "../utils/api";
import type { ApiCapabilitiesResponse } from "../components/schema/types";

export interface Capabilities {
  schema: boolean;
  /** False until the answer arrives — the rail renders no Code mode meanwhile
      rather than showing one and taking it away a moment later. */
  resolved: boolean;
}

export function useCapabilities(): Capabilities {
  const [caps, setCaps] = useState<Capabilities>({ schema: false, resolved: false });

  useEffect(() => {
    let cancelled = false;
    void attempt(() => api.get<ApiCapabilitiesResponse>("/code/capabilities")).then(([res, err]) => {
      if (cancelled) return;
      // A failed probe resolves to "no schema mode": the view would have
      // nothing to render anyway if the code index cannot be reached.
      setCaps({ schema: !err && res?.ok ? Boolean(res.data?.schema) : false, resolved: true });
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return caps;
}
