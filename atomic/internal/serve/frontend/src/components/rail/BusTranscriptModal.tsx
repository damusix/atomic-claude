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
  offset: number;
  firstEntry: number;
  lastEntry: number;
}

const PAGE = 100;

export function BusTranscriptModal({
  member,
  onClose,
}: {
  member: BusSessionInfo;
  onClose: () => void;
}) {
  const [data, setData] = useState<BusTranscriptResponse | null>(null);
  const [failed, setFailed] = useState(false);
  const [offset, setOffset] = useState(0);

  useEffect(() => {
    let cancelled = false;
    void attempt(() =>
      api.get<BusTranscriptResponse>(
        `/bus/transcript?session=${encodeURIComponent(member.session)}&n=${PAGE}&offset=${offset}`,
      ),
    ).then(([res, err]) => {
      if (cancelled) return;
      if (!err && res?.ok && res.data) setData(res.data);
      else setFailed(true);
    });
    return () => {
      cancelled = true;
    };
  }, [member.session, offset]);

  // Older pages exist while the window's first entry is not entry 1;
  // newer pages exist whenever we're offset back from the tail.
  const canOlder = data ? offset + data.shownEntries < data.totalEntries : false;
  const canNewer = offset > 0;

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
                    · entries {data.firstEntry}–{data.lastEntry} of {data.totalEntries}
                  </>
                ) : null}
              </p>
            </div>
            <div className="bus-transcript-pager">
              <button
                type="button"
                disabled={!canOlder}
                onClick={() => setOffset(offset + PAGE)}
                aria-label="Older entries"
              >
                ‹ older
              </button>
              <button
                type="button"
                disabled={!canNewer}
                onClick={() => setOffset(Math.max(0, offset - PAGE))}
                aria-label="Newer entries"
              >
                newer ›
              </button>
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
