# ─────────────────────────────────────────────────────────────────────────────
# Atomic Claude — launch demo (the three-beat "wow")
#
#   index → explore → serve.  Renders the terminal beats to a GIF; the browser
#   graph (beat 3) is captured separately (see footer) because VHS records a
#   TERMINAL only.
#
#   Install:  brew install vhs        (or: go install github.com/charmbracelet/vhs@latest)
#   Run:      vhs marketing/demo.tape    →  outputs marketing/demo.gif
#
#   VERIFIED on this machine: vhs 0.11.0, JetBrains Mono, Catppuccin Mocha.
#
#   READ FIRST — this tape RUNS the commands for real:
#     1. Set DEMO_DIR (below) to a repo to demo. For the SQL/embedded-SQL "wow"
#        you need a repo with code + .sql + SQL in string literals (a billing
#        fixture). The atomic repo itself works for a code-graph-only demo —
#        see the two query options in beat 2.
#     2. `atomic` must be on PATH (global install). To demo a repo-local build,
#        uncomment the PATH line in the Hide block.
#     3. Tune the Sleep values by eye: re-render, watch, adjust.
#
#   WHY THE atomic() WRAPPER (Hide block): vhs 0.11.0 renders the "→" glyph blank
#   regardless of font (it's an ambiguous-width codepoint quirk in its bundled
#   terminal). atomic's output uses "→". The wrapper swaps "→"→"->" for the
#   recording only (via perl, ASCII-only pattern), so arrows show up. `serve` is
#   passed through untouched so its server output streams and Ctrl+C works. This
#   affects the GIF only; the real tool still prints "→" in your terminal.
# ─────────────────────────────────────────────────────────────────────────────

Output marketing/demo.gif

# ── Look & feel (all verified to render cleanly) ─────────────────────────────
Set Shell         "zsh"
Set FontFamily    "JetBrains Mono"   # installed via: brew install --cask font-jetbrains-mono
Set FontSize      22
Set Width         1280
Set Height        800
Set Padding       28
Set Theme         "Catppuccin Mocha"
Set WindowBar     Colorful
Set BorderRadius  10
Set TypingSpeed   45ms
Set Framerate     60

# ── Off-camera setup (Hide ... Show is never recorded) ───────────────────────
Hide
# Use the repo-local build so the demo includes the latest fixes (e.g. the
# Relationships name-resolution fix). Switch to the global `atomic` once that
# ships in a release: just delete this PATH line.
Type `export PATH=/Users/alonso/projects/github/atomic-claude/bin:$PATH`
Enter
Type `cd ${DEMO_DIR:-/Users/alonso/projects/github/atomic-claude}`
Enter
# Arrow-swap wrapper (GIF only; serve passes through). ASCII-only, no literal →.
Type `atomic(){ if [ "$1" = serve ]; then command atomic "$@"; else command atomic "$@" | perl -CSAD -pe 's/\x{2192}/->/g'; fi; }`
Enter
Type `export PROMPT='%F{green}❯%f '`
Enter
Type `clear`
Enter
Show

Sleep 800ms

# ── BEAT 1 — index (~8s) ─────────────────────────────────────────────────────
# Parse the whole repo into a local SQLite symbol graph. With a SQL fixture this
# also extracts SQL out of string literals in app code.
Type "atomic code index"
Sleep 500ms
Enter
# Tune to your repo's real index time.
Sleep 7s

# ── BEAT 2 — explore (~12s), the payoff ──────────────────────────────────────
# Ask in plain English, get structure back: symbols, call paths, relationships
# (resolved to names, not node IDs).
#
# DEFAULT (works in the atomic repo, no fixture needed):
Type `atomic code explore "how does wiki staleness work"`
#
# BILLING / SQL FIXTURE variant (the real wow — embedded SQL → DB lineage).
# Point DEMO_DIR at a Go+SQL billing fixture and swap the line above for:
#   Type `atomic code explore "how does the billing pipeline work"`
Sleep 500ms
Enter
Sleep 9s          # long enough to read the digest

# ── BEAT 3 — serve (~5s), the handoff to the browser ─────────────────────────
Type "atomic serve --port 4317"
Sleep 500ms
Enter
Sleep 4s          # show the boot banner + "serving at http://localhost:4317"
Ctrl+C            # stop the server so the render ends cleanly

Sleep 1s

# ─────────────────────────────────────────────────────────────────────────────
# BEAT 3 (browser graph) — capture separately, then pair with the terminal GIF:
#   macOS:  Cmd+Shift+5 → record the browser window while you run:
#             atomic serve --open
#           then: graph loads → click the billing/wiki node → follow a wikilink.
#   Convert to GIF if needed:
#             ffmpeg -i serve.mov -vf "fps=24,scale=1280:-1:flags=lanczos" serve.gif
#   Placement:
#     README  — terminal GIF above the fold, browser GIF directly below.
#     X thread — terminal GIF on tweet 1, browser GIF as the first self-reply.
# ─────────────────────────────────────────────────────────────────────────────
