// Augments bun:test's `expect(...)` with jest-dom's DOM matchers
// (toBeInTheDocument, toHaveAttribute, toHaveTextContent, ...) for the type
// checker. The runtime registration is setup.testing.ts's `expect.extend`.
//
// jest-dom ships this exact augmentation at types/bun.d.ts, but that path
// isn't in the package's "exports" map, so bundler-mode resolution can't
// reach it from an import specifier — inlined here instead, sourced from
// the same `TestingLibraryMatchers` type the package's own bun.d.ts uses.
import { type expect } from "bun:test";
import { type TestingLibraryMatchers } from "@testing-library/jest-dom/matchers";

declare module "bun:test" {
  interface Matchers<T = unknown>
    extends TestingLibraryMatchers<ReturnType<typeof expect.stringContaining>, T> {}
}

