# atomic serve feature tour

Records a title-carded walkthrough of `atomic serve` as one 1080p mp4, one scene per surface. Everything runs headless; nothing is captured by hand. Scenes are recorded independently and stitched, so a new or re-shot scene never costs a full re-run.

## Run

```sh
marketing/demo/prep.sh                       # once: build bin/atomic, index the realm, seed a bundle, stop the bus daemon
node marketing/demo/run.mjs                  # record whatever is missing from out/scenes/, stitch out/demo.mp4
node marketing/demo/run.mjs --scene docs,search,external   # re-shoot only these, keep the rest, re-stitch
node marketing/demo/run.mjs --force          # re-shoot everything
node marketing/demo/run.mjs --stitch         # just re-concatenate
node marketing/demo/run.mjs --url http://127.0.0.1:4500 --scene chat   # drive a server you started
```

Refresh the wiki first if the pages look stale: `/refresh-wiki` in a Claude session at the realm root. That is the one preparation step `prep.sh` cannot do.

## Adding a scene

1. Create `scenes/<name>.mjs` exporting `{ name, run(page, { base, root }) }`; use the `overlay.mjs` helpers for every interaction.
2. Import it in `scenes/index.mjs` and place it in `SCENES` — that array is the tour order.
3. `node marketing/demo/run.mjs` records only the new one and re-stitches.

Optional scene fields: `viewport` (default 1920x1080), `speed` (playback multiplier applied in normalize), `tapes` + `room` (VHS terminals composed to the right of the browser), `warm(page, ctx)` (runs first in a throwaway page of the same browser context whose video is discarded — the graph scene uses it to fill the layout cache so the recorded page opens already settled).

## Pipeline

```
prep.sh            bin/atomic, atomic code index, scratchpad bundle, bus stop
  |
run.mjs            spawns bin/atomic serve --port 4317 in DEMO_ROOT
  |
  +-- per scene    [warm page]  ->  recorded page  ->  out/raw/<scene>.webm
  |                  overlay.mjs: title card, fake cursor, eased pointer travel
  |
  +-- chat scene   + two VHS tapes in parallel (tapes/agent-a.tape, agent-b.tape)
  |                  ffmpeg hstack: browser 1280px | terminals 640px stacked
  |
  +-- normalize    out/scenes/<scene>.mp4   1920x1080 H.264 30fps, speed applied, 0.4s head trim
  |
  +-- stitch       out/demo.mp4  every present scene in SCENES order (missing ones are listed)
```

| Scene | Route | What it shows |
|-------|-------|---------------|
| docs | `/` | Browse drawer tree, a member wiki page, rail Links tab (filter, kind chips), rail Graph tab, follow a link |
| search | `⌘K` | one query across md / code / plans; a code hit opens the code modal: find-in-file, callers, one hop, Back; View all results |
| schema | `/code/schema?member=` | filter to a table, source modal, "Written by" and "Read by" openers |
| chat | `/bus` | open `atomic-demo`, two Sonnet agents join from terminals and greet each other, browser greets both (2x) |
| plans | `/plans?member=` | filter, spec, scratchpad BRIEF/STATE, design, version picker |
| external | `/external` | outbound URLs by domain, citing pages, first seen |
| graph | `/graph` | docs graph already settled: shift-hover preview, ctrl-click pin, click opens page, search; same on the code graph ending in the code modal |
| about | About panel | version, bus rooms, wiki/index health, closing card |

## Knobs

All optional, read from the environment.

| Variable | Default | Used by |
|----------|---------|---------|
| `DEMO_ROOT` | `~/projects/noorm` | directory served; a realm root or a repo |
| `DEMO_DOCS_MEMBER` | `monorepo` | docs scene |
| `DEMO_SEARCH_QUERY` | `change` | search scene |
| `DEMO_PLAN_MEMBER`, `DEMO_PLAN_SLUG` | `monorepo`, `config-access-roles` | plans scene and `prep.sh` bundle seed |
| `DEMO_SCHEMA_MEMBER`, `DEMO_SCHEMA_TABLE` | `monorepo`, `Milestone` | schema scene |
| `DEMO_GRAPH_MEMBER` | `monorepo` | code graph member |
| `DEMO_SETTLE_MS` | `60000` | graph settle budget (warm-up only; the recorded page replays from cache) |

The bus room name is fixed at `atomic-demo` in both tapes and `scenes/chat.mjs`; change all three together. Before the chat scene the runner closes that room, stops the daemon, and deletes `~/.atomic/rooms/atomic-demo.log` so old traffic never appears. No other room is touched.

## What bites

- A scene that throws stops the run with the scene name, keeps `out/raw/<scene>.webm`, and writes `out/raw/<scene>.failed.png`. Fix, then `--scene <name>`.
- Playwright never draws the OS pointer. `lib/overlay.mjs` injects a cursor that follows real `mousemove` events, so use its `click`/`glide`/`type` helpers rather than `locator.click()`, or the recording shows things happening with no hand. `click` is a single `mouse.click` on purpose: a held press lets outside-pointerdown dismissers (the search palette) unmount the target before mouseup.
- Search palette rows are Ark combobox options, `[role="option"][data-value^="md:|code:"]`, and switching source clears the query, so the scene retypes it each time.
- Graph nodes are GPU-picked inside a canvas. Positions come from `window.GraphCore.debugState()`; hover is Shift-gated, pin is a real Control keydown + click. The layout cache key is the data fingerprint (plus member for code), so `warm()` must visit the same views the scene records.
- The tapes run a real `claude --model sonnet` with `--strict-mcp-config` and only `Bash(atomic bus:*)` + `Monitor` allowed. Output is nondeterministic; re-run `--scene chat` if an agent rambles. Inherited `CLAUDE_CODE_*` variables are unset in the tape's hidden preamble so a session launched from inside Claude Code does not carry its parent's banners.
- Bus member names carry the realm prefix (`noorm-agent-a`). The browser reads the roster and addresses whatever is there; the tapes tell each agent to find its peer by suffix.
- `prep.sh` seeds one scratchpad bundle (`atomic scratchpad new <slug> --purpose implement`) inside the plan member so the Plans scene has BRIEF/STATE to show. It is gitignored in that repo and left in place.
- `recordVideo.size` must equal the viewport or Playwright letterboxes. The chat scene is 1280x1080 on purpose; everything else is 1920x1080.

## Requirements

`npm ci` at the repo root (Playwright 1.61 with a cached Chromium), `brew install vhs ffmpeg`, the JetBrains Mono font for the tapes, and a `claude` on PATH for the chat scene.
