---
paths:
  - "**/*.{ts,tsx}"
---

# TypeScript style


## Readability

Write TypeScript that reads top-to-bottom without surprises. A reader should understand what a function does from its name, parameters, and the first few lines — not by tracing through three layers of abstraction. Flat is better than nested. Early returns over deep conditionals. Named variables over inline expressions when the expression is not obvious.

- Destructure at the call site when it clarifies intent. Do not destructure when it obscures where values come from.
- Prefer `const` declarations. Use `let` only when reassignment is genuinely needed. Never `var`.
- Keep functions short and focused. If a function needs a comment explaining what the next block does, that block is a separate function.
- Name things for what they represent, not how they are used. `userEmail` not `str`, `remainingAttempts` not `count`.
- Avoid clever code. A straightforward loop that a junior developer can read beats a reduce chain that saves two lines.

## Never cast

Do not use `as`. If TypeScript complains, the types are wrong or the data flow is wrong — fix that, not the symptom. The one exception: untyped external APIs with no `@types` package, and even then prefer `unknown` + a type guard over a cast. `satisfies` is acceptable when validating a literal conforms to a type without widening or narrowing.

## Type errors are signals, not noise

A type error means something is probably wrong, or something will go wrong. Treat every type error as a bug report from the compiler. Do not suppress with `any`, `@ts-ignore`, `@ts-expect-error` (unless genuinely temporary with a linked issue), or casting. If the fix feels hard, the design likely needs to change — that is the type system doing its job.

- Prefer inference over annotation when the type is obvious.
- Use `unknown` + narrowing instead of `any`. Generics over `any` for flexible APIs.
- Do not annotate obvious return types or variable types — let the compiler infer.
- Strict null checks on. No non-null assertions (`!`) unless you can prove the value exists and a guard would be dead code.

## Types in tests validate your API surface

Explicit type annotations in test files serve a different purpose than in production code. In tests, types are contracts: they prove the public API shape is correct, and they break the build when an exposed surface changes.

- Annotate function inputs and expected outputs with the public types your module exports.
- If a test compiles with a hardcoded `any`, it is not testing the type contract — rewrite it.
- When a type change breaks a test, that is the test working. Do not cast to fix it — update the test to match the new contract, or revert the type change if it was unintentional.

## Implement the feature, not the types

The goal is working business logic, not a passing type checker. Types and tests are a temporary medium through which you validate your implementation — they are not the implementation. A green CI with wrong behavior is a false positive. A type-safe function that does the wrong thing is still a bug.

- Write the feature first. Let types emerge from the shape of the data and the flow of the logic. Do not design types in advance and then force the implementation to fit them.
- Passing tests and passing types are not success. Correct behavior is success. If the feature works wrong but the types compile and the tests pass, the types and tests are wrong — not the other way around.
- Do not spend time on type gymnastics (deep conditional types, mapped type chains, template literal acrobatics) unless the consumer-facing API genuinely demands it. Most type complexity is unnecessary — simplify the data flow instead.

## Test across boundaries

Unit tests prove a function works in isolation. Integration tests prove the feature works. When the two disagree, the integration test is right.

- Integration and end-to-end tests are more valuable than unit tests. They test what you actually built, not what you think you built. Prioritize them.
- Good tests cross boundaries — they call the public API, hit the real database (or a real-enough substitute), traverse the middleware stack, and verify the response. A test that mocks every dependency is testing your mocks.
- Unit tests are appropriate for pure logic (parsers, validators, transformers) where the function has no side effects and the inputs/outputs tell the whole story. For everything else, prefer integration.
- Do not write a unit test just to get coverage. A test that cannot fail when the business logic changes is dead weight. Every test should encode an intention: "if this breaks, something the user cares about is wrong."
