# Signals steering

User-provided hints for the signals inferrer. Content here overrides pure inference.


## Structural cross-referencing with ast-grep

At Step 1, check whether `sg` (ast-grep) is available: `which sg`. If it is, include the following instruction block in every domain-writer sub-agent brief. If not, omit it entirely — sub-agents fall back to reading source files directly.

Instruction block to include in sub-agent prompts when `sg` is available:

> `sg` (ast-grep) is installed. Use it to structurally resolve dependencies and coupling for your domain. AST queries are provable — prefer them over inferring relationships from filenames or directory proximity.
>
> Useful patterns:
>
> **Go:** `sg run -p 'import ($$$IMPORTS)' -l go <paths>` for import graph. Cross-package calls: find `pkg.ExportedFunc()` sites after identifying imports. Exported API: `sg run -p 'func $NAME($$$) $$$' -l go <paths>` (filter uppercase). Types: `sg run -p 'type $NAME struct { $$$FIELDS }' -l go <paths>`.
>
> **TypeScript:** `sg run -p 'import { $$$NAMES } from "$SOURCE"' -l typescript <paths>` for imports. `sg run -p 'export function $NAME($$$) { $$$BODY }' -l typescript <paths>` for exports.
>
> **Python:** `sg run -p 'from $MODULE import $$$NAMES' -l python <paths>` for imports. `sg run -p 'def $NAME($$$PARAMS): $$$BODY' -l python <paths>` for definitions.
>
> Populate `## Coupling` sections with structurally proven dependencies from `sg` results, not guesses.
