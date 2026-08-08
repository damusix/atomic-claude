// BusTranscriptModal — renders a bus member's Claude Code session .jsonl
// as server-rendered markdown (GET /api/bus/transcript, same goldmark
// pipeline as every page — hence the md-content class and
// dangerouslySetInnerHTML, matching pages/Page).
import { useEffect, useState } from "react";
import { Dialog } from "@ark-ui/react";
import { attempt } from "@logosdx/utils";
import { api } from "../../utils/api";
import type { BusSessionInfo } from "./BusRail";

interface BusTranscriptResponse {
  html: string;
  title: string;
  agentName?: string;
  path: string;
  shownEntries: number;
  totalEntries: number;
}

export function BusTranscriptModal({
  member,
  onClose,
}: {
  member: BusSessionInfo;
  onClose: () => void;
}) {
  const [data, setData] = useState<BusTranscriptResponse | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void attempt(() =>
      api.get<BusTranscriptResponse>(`/bus/transcript?session=${encodeURIComponent(member.session)}`),
    ).then(([res, err]) => {
      if (cancelled) return;
      if (!err && res?.ok && res.data) setData(res.data);
      else setFailed(true);
    });
    return () => {
      cancelled = true;
    };
  }, [member.session]);

  return (
    <Dialog.Root
      open
      onOpenChange={(details) => {
        if (!details.open) onClose();
      }}
      aria-label="Session transcript"
    >
      <Dialog.Backdrop className="bus-transcript-backdrop" />
      <Dialog.Positioner className="bus-transcript-positioner">
        <Dialog.Content className="bus-transcript-box">
          <div className="bus-transcript-header">
            <div className="bus-transcript-heading">
              <Dialog.Title className="bus-transcript-title">
                {data?.title ?? member.session}
              </Dialog.Title>
              <p className="bus-transcript-sub">
                <strong>{member.name}</strong> · {member.session}
                {data ? (
                  <>
                    {" "}
                    · {data.shownEntries}/{data.totalEntries} entries
                  </>
                ) : null}
              </p>
            </div>
            <Dialog.CloseTrigger className="bus-transcript-close" aria-label="Close">
              ✕
            </Dialog.CloseTrigger>
          </div>
          <div className="bus-transcript-scroll">
            {failed ? (
              <p className="bus-transcript-note">Could not load the transcript.</p>
            ) : !data ? (
              <p className="bus-transcript-note">Loading…</p>
            ) : (
              <div
                className="md-content bus-transcript-body"
                dangerouslySetInnerHTML={{ __html: data.html }}
              />
            )}
          </div>
          {data ? <p className="bus-transcript-path">{data.path}</p> : null}
        </Dialog.Content>
      </Dialog.Positioner>
    </Dialog.Root>
  );
}
