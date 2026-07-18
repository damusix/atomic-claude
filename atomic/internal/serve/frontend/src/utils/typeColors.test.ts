import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { atomicCyTypeColors, atomicRampColors, installTypeColorsGlobal } from "./typeColors";

// The exact CSS custom properties app.css sets for a subset of the palette —
// enough to prove atomicCyTypeColors reads through them the same way
// templates/layout.html's original atomicCyTypeColors() does.
const CSS_VARS: Record<string, string> = {
  "--ink": "#211d18",
  "--paper-raised": "#ffffff",
  "--hairline-2": "#e2ddd2",
  "--edge": "#cabfae",
  "--edge-strong": "#b1a48f",
  "--amber-bright": "#d99a1f",
  "--ramp-gold-dusky-1": "#bfa45e",
  "--ramp-gold-dusky-2": "#a4873f",
  "--ramp-gold-dusky-3": "#886e30",
  "--c-page": "#a4873f",
  "--c-page-ink": "#bfa45e",
};

function applyCSSVars(vars: Record<string, string>) {
  for (const [name, value] of Object.entries(vars)) {
    document.documentElement.style.setProperty(name, value);
  }
}

describe("typeColors", () => {
  beforeEach(() => {
    applyCSSVars(CSS_VARS);
  });

  afterEach(() => {
    document.documentElement.removeAttribute("style");
  });

  test("atomicCyTypeColors reads fill/ink from the same CSS vars as the carried atomicCyTypeColors()", () => {
    const colors = atomicCyTypeColors();
    expect(colors.page).toBe("#a4873f");
    expect(colors["page-ink"]).toBe("#bfa45e");
    expect(colors["default-label"]).toBe("#211d18");
    expect(colors["label-bg"]).toBe("#ffffff");
    expect(colors.edge).toBe("#cabfae");
    expect(colors.selected).toBe("#d99a1f");
  });

  test("default-fill mirrors the page type, matching the carried fallback convention", () => {
    const colors = atomicCyTypeColors();
    expect(colors["default-fill"]).toBe(colors.page);
    expect(colors["default-dark"]).toBe(colors["page-dark"]);
  });

  test("falls back to the dark-theme ramp when a --c-<type> var is missing", () => {
    document.documentElement.style.removeProperty("--c-page");
    document.documentElement.style.removeProperty("--c-page-ink");
    const colors = atomicCyTypeColors();
    // shade 2 of the dusky ramp is the documented fallback for fill.
    expect(colors.page).toBe("#a4873f");
    expect(colors["page-ink"]).toBe("#bfa45e");
  });

  test("falls back to RAMP_FALLBACK's hardcoded dark-theme values when the CSS var itself is absent", () => {
    document.documentElement.removeAttribute("style");
    const ramps = atomicRampColors();
    expect(ramps["gold-dusky-1"]).toBe("#d6c08d");
  });

  test("installTypeColorsGlobal exposes bare-callable globals for the carried vanilla scripts", () => {
    const fakeWindow = {} as Window;
    installTypeColorsGlobal(fakeWindow);
    expect(fakeWindow.atomicCyTypeColors).toBe(atomicCyTypeColors);
    expect(fakeWindow.atomicRampColors).toBe(atomicRampColors);
  });
});
