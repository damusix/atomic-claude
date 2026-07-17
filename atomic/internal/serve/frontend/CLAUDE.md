# frontend/ — atomic serve React app


React + TypeScript SPA for `atomic serve`. Contract: `docs/spec/serve-react-frontend.md`. Deliberation: `docs/design/serve-react-frontend.md`.


## Toolchain: Bun only


- Bun is the package manager, bundler, and test runner. Never npm, pnpm, yarn, Vite, Jest, or Vitest.
- `bun install` / `bun add` for dependencies; `bun test` for tests; `bun build` for bundling into `dist/`.
- Least dependencies wins: Bun's built-ins (bundling, testing, TS/JSX transpile) beat any dev-dep that duplicates them. Justify every `bun add` against what Bun already ships.
- Build-pipeline wiring (`make frontend`, drift gate, embed) is owned by the spec's CP1 — this file governs conventions, not the pipeline.


## Layout


    src/
    ├── components/   # component folders — rules below
    ├── layouts/      # app shells composing panes (three-pane Obsidian shell)
    ├── pages/        # route-level screens (page, graph, search, status, external)
    ├── hooks/        # shared hooks (useLiveReload, useTheme, ...)
    └── utils/        # shared non-React logic (typeColors, graphEngineAdapter, ...)

Everything is scoped by domain: a file lives with the domain that owns it. The five top-level buckets are the only type-based split; below them, group by feature.


## Component rules


- One folder per component: `src/components/<name>/{style.css, *.ts, *.tsx, utils/}` — turtles all the way down: a subcomponent that grows gets its own nested folder with the same shape.
- `components/ui/**` holds generic, app-agnostic UI primitives (button, modal, spinner, ...), barrel-exported from `components/ui/index.ts`. Consumers import from the barrel, never deep paths.
- Every other `components/<name>/` is app-specific (nav, rail, search, code-modal, schema, ...). No barrels — import directly from the file.
- Styles: a per-component `style.css` beside the component. The design system lives in the carried `app.css` custom properties — component CSS consumes those variables, never redefines palette values.


## Carried code (do not rebuild)


`public/` holds carried-verbatim assets: `graph-core.js`, `system-graph.js`, `code-graph.js`, vendored cosmos.gl / Cytoscape / mermaid, `app.css`. Vanilla JS by design — outside the TS/bundle scope. React talks to it only through the window contracts: `GraphCore`, `AtomicGraphUI`, `AtomicCodeExplorer`, and the `typeColors` global.
