import { describe, expect, test } from "bun:test";
import { dedupeEdges, edgeKind, edgeMatches, pathKind } from "./edges";
import type { RailEdge } from "./types";

function edge(over: Partial<RailEdge> = {}): RailEdge {
  return {
    target: "notes.md",
    resolvedPath: "docs/notes.md",
    broken: false,
    dir: false,
    ambiguous: false,
    codeFile: false,
    external: false,
    ...over,
  };
}

describe("edgeKind", () => {
  test("buckets by extension", () => {
    expect(edgeKind(edge({ resolvedPath: "atomic/main.go" }))).toBe("go");
    expect(edgeKind(edge({ resolvedPath: "docs/notes.md" }))).toBe("md");
  });

  // Directories and external links have no extension to bucket by, and a
  // "folder" filter is the one a reader actually reaches for.
  test("gives directories and external links their own buckets", () => {
    expect(edgeKind(edge({ dir: true, resolvedPath: "atomic/internal/bus" }))).toBe("folder");
    expect(edgeKind(edge({ external: true, target: "https://example.com/x" }))).toBe("link");
  });

  // Makefile and Dockerfile would each become a single-entry bucket if the
  // whole filename were used, cluttering the chip row to no purpose.
  test("groups extensionless files under one bucket", () => {
    expect(edgeKind(edge({ resolvedPath: "atomic/Makefile" }))).toBe("file");
    expect(pathKind("Dockerfile")).toBe("file");
  });
});

describe("edgeMatches", () => {
  const view = dedupeEdges([edge({ resolvedPath: "atomic/internal/serve/render.go" })])[0];

  test("an empty query matches everything", () => {
    expect(edgeMatches(view, "")).toBe(true);
  });

  test("matches case-insensitively on the visible name", () => {
    expect(edgeMatches(view, "RENDER")).toBe(true);
  });

  // The row shows only the filename, so a search restricted to it would fail
  // on the directory the reader is actually thinking of.
  test("matches a path segment the row does not display", () => {
    expect(edgeMatches(view, "internal/serve")).toBe(true);
  });

  test("does not match an unrelated term", () => {
    expect(edgeMatches(view, "postgres")).toBe(false);
  });
});
