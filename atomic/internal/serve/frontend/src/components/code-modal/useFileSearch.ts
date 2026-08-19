// Find-in-file for the source pane. /api/file/ renders the whole file in one
// shot with no virtualisation, so this searches the DOM already on screen.
//
// Matches are Ranges painted with the CSS Custom Highlight API rather than
// <mark> wrappers: chroma splits each line into per-token spans, so a match like
// `nodes[i].Name` straddles them, and a Range crosses that for free. Without the
// API matches still count and scroll; only the paint is lost.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

const HIGHLIGHT_ALL = "code-find";
const HIGHLIGHT_CURRENT = "code-find-current";

/** Every match is a live Range the browser repaints on scroll, and a short
 *  query against a minified file matches tens of thousands of times. */
const MAX_MATCHES = 2000;

export interface FileSearch {
    total: number;
    /** 1-based position of the match in view; 0 when there are none. */
    current: number;
    /** The cap stopped the count short of the real total. */
    truncated: boolean;
    /** Step the viewport by delta matches, wrapping at both ends. */
    go: (delta: number) => void;
}

/** A text node and where its text starts in the concatenated file text. */
interface Chunk {
    node: Text;
    start: number;
}

const supportsHighlights = () =>
    typeof CSS !== "undefined" && "highlights" in CSS && typeof Highlight !== "undefined";

/** Skips the line-number column: without it a query of "12" matches the gutter. */
function collectChunks(container: HTMLElement): { chunks: Chunk[]; text: string } {
    const chunks: Chunk[] = [];
    let text = "";

    for (const cell of container.querySelectorAll<HTMLElement>("td.ld")) {
        const walker = document.createTreeWalker(cell, NodeFilter.SHOW_TEXT);
        let node = walker.nextNode();
        while (node) {
            const value = node.nodeValue ?? "";
            if (value.length > 0) {
                chunks.push({ node: node as Text, start: text.length });
                text += value;
            }
            node = walker.nextNode();
        }
        // Without this a query matches across the end of one line into the next.
        text += "\n";
    }

    return { chunks, text };
}

/** An offset span becomes a Range that may start and end in different tokens. */
function rangeFor(chunks: Chunk[], from: number, to: number): Range | null {
    let startIdx = -1;
    for (let i = 0; i < chunks.length; i++) {
        const chunk = chunks[i];
        const end = chunk.start + (chunk.node.nodeValue?.length ?? 0);
        if (from >= chunk.start && from < end) {
            startIdx = i;
            break;
        }
    }
    if (startIdx < 0) return null;

    const range = document.createRange();
    range.setStart(chunks[startIdx].node, from - chunks[startIdx].start);

    for (let i = startIdx; i < chunks.length; i++) {
        const chunk = chunks[i];
        const end = chunk.start + (chunk.node.nodeValue?.length ?? 0);
        if (to <= end) {
            range.setEnd(chunk.node, to - chunk.start);
            return range;
        }
    }
    return null;
}

export function useFileSearch(
    containerRef: React.RefObject<HTMLElement | null>,
    query: string,
): FileSearch {
    const [ranges, setRanges] = useState<Range[]>([]);
    const [truncated, setTruncated] = useState(false);
    const [index, setIndex] = useState(0);

    // Switching files must re-run the search; the old Ranges point at detached
    // nodes.
    const [revision, setRevision] = useState(0);

    const needle = query.trim().toLowerCase();

    useEffect(() => {
        const container = containerRef.current;
        if (!container) return;
        const observer = new MutationObserver(() => setRevision((r) => r + 1));
        observer.observe(container, { childList: true, subtree: true });
        return () => observer.disconnect();
    }, [containerRef]);

    useEffect(() => {
        const container = containerRef.current;
        if (!container || needle === "") {
            setRanges([]);
            setTruncated(false);
            setIndex(0);
            return;
        }

        const { chunks, text } = collectChunks(container);
        const haystack = text.toLowerCase();

        const found: Range[] = [];
        let cut = false;
        let at = haystack.indexOf(needle);
        while (at !== -1) {
            if (found.length >= MAX_MATCHES) {
                cut = true;
                break;
            }
            const range = rangeFor(chunks, at, at + needle.length);
            if (range) found.push(range);
            at = haystack.indexOf(needle, at + needle.length);
        }

        setRanges(found);
        setTruncated(cut);
        setIndex(0);
    }, [containerRef, needle, revision]);

    // The highlight registry is global, so a closed modal would stay lit.
    useEffect(() => {
        if (!supportsHighlights()) return;
        if (ranges.length === 0) {
            CSS.highlights.delete(HIGHLIGHT_ALL);
            CSS.highlights.delete(HIGHLIGHT_CURRENT);
            return;
        }
        CSS.highlights.set(HIGHLIGHT_ALL, new Highlight(...ranges));
        const active = ranges[index];
        if (active) CSS.highlights.set(HIGHLIGHT_CURRENT, new Highlight(active));
        return () => {
            CSS.highlights.delete(HIGHLIGHT_ALL);
            CSS.highlights.delete(HIGHLIGHT_CURRENT);
        };
    }, [ranges, index]);

    // The row is scrolled, not the Range: centring a token loses its line.
    useEffect(() => {
        const active = ranges[index];
        if (!active) return;
        const row =
            active.startContainer.parentElement?.closest("tr") ??
            active.startContainer.parentElement;
        row?.scrollIntoView({ block: "center" });
    }, [ranges, index]);

    const go = useCallback(
        (delta: number) => {
            setIndex((i) => {
                if (ranges.length === 0) return 0;
                return (i + delta + ranges.length) % ranges.length;
            });
        },
        [ranges.length],
    );

    return useMemo(
        () => ({
            total: ranges.length,
            current: ranges.length === 0 ? 0 : index + 1,
            truncated,
            go,
        }),
        [ranges.length, index, truncated, go],
    );
}

export function useFocusOnFindKey(ref: React.RefObject<HTMLInputElement | null>, active: boolean) {
    const ourRef = useRef(ref);
    ourRef.current = ref;

    useEffect(() => {
        if (!active) return;
        const onKey = (e: KeyboardEvent) => {
            // Only while the modal owns the screen; the browser's own find is
            // left alone everywhere else.
            if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "f") {
                e.preventDefault();
                ourRef.current.current?.focus();
                ourRef.current.current?.select();
            }
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [active]);
}
