---
paths:
  - "**/*.py"
---

# Python style


Write Python that reads top-to-bottom without surprises. Type-hint public functions and dataclasses; rely on inference inside small local scopes. Prefer dataclasses and `TypedDict` over loose dicts when shape matters. Use `pathlib.Path` over `os.path`, f-strings over `%` and `.format()`, comprehensions over `map`/`filter` when the body is short. Raise specific exceptions; never `except:` bare. Keep functions small and pure where you can, isolate I/O at the edges. Follow PEP 8 for layout; ruff or black settles disputes.
