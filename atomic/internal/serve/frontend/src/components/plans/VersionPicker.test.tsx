import { describe, expect, mock, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { typeIntoCombobox } from "../../test/typeIntoCombobox";
import type { PlanDocVersion } from "../../utils/plansApi";
import { VersionPicker } from "./VersionPicker";

function checkout(overrides: Partial<PlanDocVersion["checkouts"][number]> = {}): PlanDocVersion["checkouts"][number] {
  return {
    id: "w1",
    branch: "main",
    path: ".",
    outsideRoot: false,
    isMain: true,
    fileMtime: "2026-08-19T00:00:00Z",
    ...overrides,
  };
}

const SINGLE: PlanDocVersion[] = [
  { sha: "a1", label: "main", isMain: true, mtime: "2026-08-19T00:00:00Z", checkouts: [checkout()] },
];

const MULTI: PlanDocVersion[] = [
  { sha: "a1", label: "main", isMain: true, mtime: "2026-08-19T00:00:00Z", checkouts: [checkout({ id: "w1", branch: "main", created: undefined })] },
  {
    sha: "b2",
    label: "scope-marker-docs",
    isMain: false,
    mtime: "2026-08-19T00:00:00Z",
    checkouts: [
      checkout({
        id: "w2",
        branch: "scope-marker-docs",
        path: ".claude/worktrees/scope-marker-docs",
        isMain: false,
        created: "2026-07-29T00:00:00Z",
        fileMtime: "2026-07-29T00:00:00Z",
      }),
    ],
  },
  {
    // deslop's checkout carries the branches ["deslop", "main"] — matching
    // deslop must find THIS version even though its representative label
    // is "main" (the merged checkout wins the label when it's in the set).
    sha: "c3",
    label: "main",
    isMain: true,
    mtime: "2026-08-16T00:00:00Z",
    checkouts: [checkout({ id: "w3", branch: "deslop", path: ".claude/worktrees/deslop", isMain: false, created: "2026-08-16T00:00:00Z", fileMtime: "2026-08-16T00:00:00Z" })],
  },
  {
    sha: "d4",
    label: "spike-doctor",
    isMain: false,
    mtime: "2026-08-02T00:00:00Z",
    checkouts: [
      checkout({
        id: "w4",
        branch: "spike-doctor",
        path: "/Users/alonso/scratch/spike-doctor",
        outsideRoot: true,
        isMain: false,
        created: "2026-08-02T00:00:00Z",
        fileMtime: undefined as unknown as string,
      }),
    ],
  },
];

describe("VersionPicker", () => {
  test("renders no picker for a single-version doc", () => {
    render(<VersionPicker versions={SINGLE} active={SINGLE[0]} onSelect={mock()} />);
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  test("multi-version doc renders one entry per version with the merged dot filled", async () => {
    render(<VersionPicker versions={MULTI} active={MULTI[0]} onSelect={mock()} />);
    const input = screen.getByRole("combobox");
    await userEvent.click(input);

    const options = screen.getAllByRole("option");
    expect(options.length).toBe(4);
    const filled = document.querySelectorAll(".vopt-dot[data-filled]");
    expect(filled.length).toBe(2); // two versions are isMain (main + deslop's, both labelled "main")
  });

  test("a missing created line is omitted rather than faked", async () => {
    render(<VersionPicker versions={MULTI} active={MULTI[0]} onSelect={mock()} />);
    await userEvent.click(screen.getByRole("combobox"));

    // MULTI[0] (main / w1) carries no `created` — its entry must not print
    // the word "created" anywhere, while it can still show "updated".
    const mainOption = screen.getAllByRole("option")[0];
    expect(mainOption.textContent).not.toMatch(/created/);
  });

  test("typing a non-label checkout name matches its version", async () => {
    render(<VersionPicker versions={MULTI} active={MULTI[0]} onSelect={mock()} />);
    const input = screen.getByRole("combobox");
    await userEvent.click(input);
    await userEvent.clear(input);
    await typeIntoCombobox(input, "deslop");

    const options = screen.getAllByRole("option");
    expect(options.length).toBe(1);
    expect(options[0].textContent).toMatch(/main/);
  });

  test("an out-of-root checkout renders an absolute path with its marker", async () => {
    render(<VersionPicker versions={MULTI} active={MULTI[0]} onSelect={mock()} />);
    const input = screen.getByRole("combobox");
    await userEvent.click(input);
    await userEvent.clear(input);
    await typeIntoCombobox(input, "spike");

    const option = screen.getByRole("option");
    expect(option.textContent).toContain("/Users/alonso/scratch/spike-doctor");
    expect(option.textContent).toMatch(/outside root/i);
  });

  test("no option is ever disabled or aria-disabled", async () => {
    render(<VersionPicker versions={MULTI} active={MULTI[0]} onSelect={mock()} />);
    await userEvent.click(screen.getByRole("combobox"));

    for (const option of screen.getAllByRole("option")) {
      expect(option).not.toHaveAttribute("disabled");
      expect(option).not.toHaveAttribute("aria-disabled", "true");
    }
  });

  test("focus clears the input so the full candidate list shows, not just the active entry", async () => {
    render(<VersionPicker versions={MULTI} active={MULTI[1]} onSelect={mock()} />);
    const input = screen.getByRole("combobox");
    expect(input).toHaveValue("scope-marker-docs");
    // An input value left as the active label filters the dropdown to that
    // one entry — the bug this guards is main unreachable once
    // selfupdate-state is active.
    await userEvent.click(input);
    expect(input).toHaveValue("");
  });

  test("blur with an empty input restores the active label instead of staying blank", async () => {
    render(<VersionPicker versions={MULTI} active={MULTI[1]} onSelect={mock()} />);
    const input = screen.getByRole("combobox");
    await userEvent.click(input);
    await userEvent.tab();
    expect(input).toHaveValue("scope-marker-docs");
  });

  test("deslop still matches to the main-labelled version after a focus/blur cycle", async () => {
    render(<VersionPicker versions={MULTI} active={MULTI[0]} onSelect={mock()} />);
    const input = screen.getByRole("combobox");
    await userEvent.click(input);
    await typeIntoCombobox(input, "deslop");

    const options = screen.getAllByRole("option");
    expect(options.length).toBe(1);
    expect(options[0].textContent).toMatch(/main/);
  });

  test("picking an entry calls onSelect with that version", async () => {
    const onSelect = mock();
    render(<VersionPicker versions={MULTI} active={MULTI[0]} onSelect={onSelect} />);
    const input = screen.getByRole("combobox");
    await userEvent.click(input);
    await userEvent.clear(input);
    await typeIntoCombobox(input, "scope-marker");
    await userEvent.click(screen.getByRole("option"));

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect.mock.calls[0][0].sha).toBe("b2");
  });
});
