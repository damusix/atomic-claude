---
paths:
  - "**/*.py"
---

# Python style


## Readability and conventions

Write Python that reads top-to-bottom without surprises. Prefer dataclasses and `TypedDict` over loose dicts when shape matters. Use `pathlib.Path` over `os.path`, f-strings over `%` and `.format()`, comprehensions over `map`/`filter` when the body is short. Raise specific exceptions; never `except:` bare. Keep functions small and pure where you can, isolate I/O at the edges. Follow PEP 8 for layout; ruff or black settles disputes.

## Type hints are guardrails, not the goal

Type-hint public functions, dataclass fields, and module boundaries. Inside small local scopes, let the reader (and mypy/pyright) infer. Do not over-annotate obvious assignments (`x: int = 5`) or litter `cast()` calls to silence the checker.

- A type error from mypy/pyright means something is probably wrong, or something will go wrong. Fix the data flow, not the annotation.
- Do not use `# type: ignore` unless genuinely temporary with a linked issue. Do not use `Any` as an escape hatch — use `object` or a protocol/generic when the type is truly dynamic.
- `cast()` is Python's `as` — it tells the checker to trust you instead of proving you are right. Avoid it. Narrow with `isinstance`, `TypeGuard`, or restructure the code.
- Type complexity (overloaded signatures, recursive generics, `ParamSpec` gymnastics) is justified only when the consumer-facing API demands it. Most type complexity means the data flow is too tangled — simplify that instead.

## Type hints in tests validate your API surface

In test files, explicit type annotations serve as contracts. They prove the public API shape is correct and break when an exposed surface changes.

- Annotate inputs and expected outputs with the public types your module exports.
- If a test passes with `Any` everywhere, it is not testing the type contract — rewrite it.
- When a type change breaks a test, that is the test working. Update the test to match the new contract, or revert the type change if unintentional.

## Implement the feature, not the types

The goal is working business logic, not a passing type checker. Types and tests are a temporary medium through which you validate your implementation — they are not the implementation. A green CI with wrong behavior is a false positive. A type-safe function that does the wrong thing is still a bug.

- Write the feature first. Let types emerge from the shape of the data and the flow of the logic. Do not design elaborate type hierarchies in advance and then force the implementation to fit them.
- Passing tests and passing mypy are not success. Correct behavior is success. If the feature works wrong but the types check and the tests pass, the types and tests are wrong.
- Do not spend time on type gymnastics (deep generics, protocol towers, overload chains) unless the consumer-facing API genuinely demands it. Most type complexity is unnecessary — simplify the data flow instead.

## Test across boundaries

Unit tests prove a function works in isolation. Integration tests prove the feature works. When the two disagree, the integration test is right.

- Integration and end-to-end tests are more valuable than unit tests. They test what you actually built, not what you think you built. Prioritize them.
- Good tests cross boundaries — they call the public API, hit the real database (or a real-enough substitute), traverse the middleware stack, and verify the response. A test that mocks every dependency with `unittest.mock.patch` is testing your mocks.
- Unit tests are appropriate for pure logic (parsers, validators, transformers) where the function has no side effects and the inputs/outputs tell the whole story. For everything else, prefer integration.
- Do not write a test just to get coverage. A test that cannot fail when the business logic changes is dead weight. Every test should encode an intention: "if this breaks, something the user cares about is wrong."
