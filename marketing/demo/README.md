# atomic serve feature tour

Records a narrated-by-title-card walkthrough of `atomic serve` as one 1080p mp4: wiki docs, SQL schema, the message bus with two live Claude agents, plans, and the network graph. Everything runs headless; nothing is captured by hand.

## Run

```sh
marketing/demo/prep.sh                 # once: build bin/atomic, index the realm, seed a bundle, stop the bus daemon
node marketing/demo/run.mjs            # full tour -> marketing/demo/out/demo.mp4
```

Subsets and a server you already started:

```sh
node marketing/demo/run.mjs --scene docs,graph         # per-scene webm in out/, no concat
node marketing/demo/run.mjs --url http://127.0.0.1:4500 --scene chat
```

Refresh the wiki first if the pages look stale: `/refresh-wiki` in a Claude session at the realm root. That is the one preparation step `prep.sh` cannot do.

## Pipeline

```
prep.sh            bin/atomic, atomic code index, scratchpad bundle, bus stop
  |
run.mjs            spawns bin/atomic serve --port 4317 in DEMO_ROOT
  |
  +-- per scene    Playwright context with recordVideo -> out/NN-<scene>.webm
  |                  overlay.mjs: title card, fake cursor, eased pointer travel
  |
  +-- chat scene   + two VHS tapes in parallel (tapes/agent-a.tape, agent-b.tape)
  |                  ffmpeg hstack: browser 1280px | terminals 640px stacked
  |
  +-- concat       ffmpeg: every scene -> 1920x1080 H.264 30fps -> out/demo.mp4
```

| Scene | Route | What it shows |
|-------|-------|---------------|
| docs | `/` -> member wiki | rendered page, rail Links tab (filter, kind chips), follow a link, rail Graph tab |
| schema | `/code/schema?member=` | filter to a table, source modal, "Written by" and "Read by" openers |
| chat | `/bus` | open `atomic-demo`, two Sonnet agents join from terminals and greet each other, browser greets both |
| plans | `/plans?member=` | filter, spec, scratchpad BRIEF/STATE, design, version picker |
| graph | `/graph` | docs graph settle, shift-hover preview, shift-drag, search, code graph for a member |

## Knobs

All optional, read from the environment.

| Variable | Default | Used by |
|----------|---------|---------|
| `DEMO_ROOT` | `~/projects/noorm` | directory served; a realm root or a repo |
| `DEMO_PLAN_MEMBER`, `DEMO_PLAN_SLUG` | `monorepo`, `config-access-roles` | plans scene and `prep.sh` bundle seed |
| `DEMO_SCHEMA_MEMBER`, `DEMO_SCHEMA_TABLE` | `monorepo`, `Milestone` | schema scene |
| `DEMO_GRAPH_MEMBER` | `monorepo` | code graph member |
| `DEMO_SETTLE_MS` | `40000` | graph settle budget |

The bus room name is fixed at `atomic-demo` in both tapes and `scenes/chat.mjs`; change all three together. Before the chat scene the runner closes that room, stops the daemon, and deletes `~/.atomic/rooms/atomic-demo.log` so old traffic never appears. No other room is touched.

## What bites

- Playwright never draws the OS pointer. `lib/overlay.mjs` injects a cursor that follows real `mousemove` events, so use its `click`/`glide`/`type` helpers rather than `locator.click()`, or the recording shows things happening with no hand.
- Graph nodes are GPU-picked inside a canvas. Positions come from `window.GraphCore.debugState()`; hover and drag are Shift-gated. Settle is ~5-10 s with GPU; under software rendering it can exceed the budget, in which case the scene records a still-moving layout.
- The tapes run a real `claude --model sonnet` with `--strict-mcp-config` and only `Bash(atomic bus:*)` + `Monitor` allowed. Output is nondeterministic; re-run the chat scene alone if an agent rambles. Inherited `CLAUDE_CODE_*` variables are unset in the tape's hidden preamble so a session launched from inside Claude Code does not carry its parent's banners.
- Bus member names carry the realm prefix (`noorm-agent-a`). The browser reads the roster and addresses whatever is there; the tapes tell each agent to find its peer by suffix.
- `prep.sh` seeds one scratchpad bundle (`atomic scratchpad new <slug> --purpose implement`) inside the plan member so the Plans scene has BRIEF/STATE to show. It is gitignored in that repo and left in place.
- `recordVideo.size` must equal the viewport or Playwright letterboxes. The chat scene is 1280x1080 on purpose; everything else is 1920x1080.

## Requirements

`npm ci` at the repo root (Playwright 1.61 with a cached Chromium), `brew install vhs ffmpeg`, the JetBrains Mono font for the tapes, and a `claude` on PATH for the chat scene.
