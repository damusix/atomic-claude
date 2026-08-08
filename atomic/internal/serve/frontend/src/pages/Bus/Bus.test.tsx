// parseComposer is the addressing contract between what the operator types
// and the envelope's to list — leading @fragments address, everything else
// is FYI body.
import { describe, expect, test } from "bun:test";
import { parseComposer } from "./Bus";

describe("parseComposer", () => {
  test("plain text is an FYI message", () => {
    expect(parseComposer("deploying to staging in 5")).toEqual({
      to: [],
      text: "deploying to staging in 5",
    });
  });

  test("leading @fragments become addressees", () => {
    expect(parseComposer("@fe @be run the tests")).toEqual({
      to: ["fe", "be"],
      text: "run the tests",
    });
  });

  test("an @ mid-sentence stays in the body", () => {
    expect(parseComposer("@fe ping me @home later")).toEqual({
      to: ["fe"],
      text: "ping me @home later",
    });
  });

  test("a bare @ is body, not an empty addressee", () => {
    expect(parseComposer("@ what")).toEqual({ to: [], text: "@ what" });
  });

  test("whitespace-only input yields empty text", () => {
    expect(parseComposer("   ")).toEqual({ to: [], text: "" });
  });
});
