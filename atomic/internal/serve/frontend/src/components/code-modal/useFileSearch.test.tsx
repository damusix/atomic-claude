// Find-in-file. The two things worth pinning are the ones a naive text search
// gets wrong on this markup: the line-number gutter is text too, and chroma
// splits a line into per-token spans so a match can straddle them.
import { describe, expect, test } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRef, useState } from "react";
import { useFileSearch } from "./useFileSearch";

/** Mirrors render.go's wrapWithLineAnchors: one row per line, the number in
 *  td.ln and the highlighted code in td.ld. */
function Harness({ rows }: { rows: string[][] }) {
    const ref = useRef<HTMLDivElement | null>(null);
    const [query, setQuery] = useState("");
    const find = useFileSearch(ref, query);

    return (
        <div>
            <input aria-label="find" value={query} onChange={(e) => setQuery(e.target.value)} />
            <span data-testid="count">{find.total}</span>
            <span data-testid="current">{find.current}</span>
            <button type="button" onClick={() => find.go(1)}>
                next
            </button>
            <div ref={ref}>
                <table className="file-view">
                    <tbody>
                        {rows.map((tokens, i) => (
                            <tr key={i} id={`L${i + 1}`}>
                                <td className="ln">
                                    <a href={`#L${i + 1}`}>{i + 1}</a>
                                </td>
                                <td className="ld">
                                    {/* One span per token, as chroma emits. */}
                                    {tokens.map((t, j) => (
                                        <span key={j}>{t}</span>
                                    ))}
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </div>
    );
}

/** Sets the query directly rather than typing it: userEvent.type reads "[" and
 *  "{" as key-descriptor syntax, so a realistic query like nodes[i].Name would
 *  never reach the input intact. */
function search(term: string) {
    fireEvent.change(screen.getByLabelText("find"), { target: { value: term } });
}

describe("useFileSearch", () => {
    test("never matches the line-number gutter", async () => {
        // "12" appears as a line number on row 12 and nowhere in the code.
        const rows = Array.from({ length: 15 }, () => ["const x = 0"]);
        render(<Harness rows={rows} />);

        search("12");

        expect(screen.getByTestId("count").textContent).toBe("0");
    });

    test("matches across the token spans chroma splits a line into", async () => {
        // A single identifier, cut into three spans the way syntax colouring
        // cuts one: searching the whole thing must still find it.
        render(<Harness rows={[["nodes[i]", ".", "Name"]]} />);

        search("nodes[i].Name");

        expect(screen.getByTestId("count").textContent).toBe("1");
    });

    test("does not run a match across a line boundary", async () => {
        // "abc" then "def" on separate lines is not "abcdef".
        render(<Harness rows={[["abc"], ["def"]]} />);

        search("abcdef");

        expect(screen.getByTestId("count").textContent).toBe("0");
    });

    test("counts every occurrence and steps through them, wrapping", async () => {
        render(<Harness rows={[["foo bar"], ["foo baz"], ["nothing"]]} />);

        search("foo");
        expect(screen.getByTestId("count").textContent).toBe("2");
        expect(screen.getByTestId("current").textContent).toBe("1");

        await userEvent.click(screen.getByText("next"));
        expect(screen.getByTestId("current").textContent).toBe("2");

        // Past the end comes back to the first, so stepping never dead-ends.
        await userEvent.click(screen.getByText("next"));
        expect(screen.getByTestId("current").textContent).toBe("1");
    });

    test("is case-insensitive", async () => {
        render(<Harness rows={[["FooBar"]]} />);

        search("foobar");

        expect(screen.getByTestId("count").textContent).toBe("1");
    });

    test("an empty query matches nothing rather than everything", async () => {
        render(<Harness rows={[["anything"]]} />);

        search("   ");

        expect(screen.getByTestId("count").textContent).toBe("0");
        expect(screen.getByTestId("current").textContent).toBe("0");
    });
});
