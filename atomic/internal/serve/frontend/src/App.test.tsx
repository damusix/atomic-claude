import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { App } from "./App";

describe("App", () => {
  test("renders the app shell mount point", () => {
    const html = renderToStaticMarkup(<App />);
    expect(html).toContain("app-root");
  });
});
