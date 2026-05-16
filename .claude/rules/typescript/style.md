---
paths:
  - "**/*.{ts,tsx}"
---

# TypeScript style


Write TypeScript that leans on the compiler instead of fighting it. Prefer inference over annotation where the type is obvious, reach for `unknown` + narrowing instead of `any`, and never use `as` to silence an error — fix the type or the data flow at the source. Use `satisfies` when validating a literal against a type without widening. Generics over `any` for flexible APIs. Strict null checks on. Treat type errors as bugs telling you the code is wrong, not noise to suppress.
