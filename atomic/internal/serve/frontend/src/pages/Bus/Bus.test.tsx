// The composer's addressing contract is chips-first: mentionQuery decides
// when the member dropdown is open, chipCommit turns a space-completed
// leading mention into a chip, and resolveMember mirrors the daemon's
// --to fragment resolution for chip display.
import { describe, expect, test } from "bun:test";
import { chipCommit, mentionQuery, resolveMember } from "./Bus";

describe("mentionQuery", () => {
  test("bare @ opens the dropdown with an empty fragment", () => {
    expect(mentionQuery("@")).toBe("");
  });

  test("typing after @ narrows the fragment", () => {
    expect(mentionQuery("@fa")).toBe("fa");
  });

  test("closed once the body starts or when there is no leading @", () => {
    expect(mentionQuery("@gui-fe hello")).toBeNull();
    expect(mentionQuery("hello @fe")).toBeNull();
    expect(mentionQuery("")).toBeNull();
  });
});

describe("chipCommit", () => {
  test("a space-terminated leading mention commits", () => {
    expect(chipCommit("@fable ")).toEqual({ chip: "fable", rest: "" });
  });

  test("body typed after the space is preserved", () => {
    expect(chipCommit("@fable hello there")).toEqual({ chip: "fable", rest: "hello there" });
  });

  test("no commit while the mention is still being typed", () => {
    expect(chipCommit("@fable")).toBeNull();
    expect(chipCommit("plain text")).toBeNull();
    expect(chipCommit("@ ")).toBeNull();
  });
});

describe("resolveMember", () => {
  const names = ["gui-fe", "gui-web", "gui-web-2", "api-be"];

  test("exact name wins even when a sibling also substring-matches", () => {
    expect(resolveMember(names, "gui-web")).toBe("gui-web");
  });

  test("unique substring resolves", () => {
    expect(resolveMember(names, "be")).toBe("api-be");
  });

  test("ambiguous fragment resolves to null", () => {
    expect(resolveMember(names, "web")).toBeNull();
  });

  test("no match resolves to null", () => {
    expect(resolveMember(names, "zzz")).toBeNull();
  });
});
