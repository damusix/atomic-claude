---
description: Compress a prose Markdown / text file into atomic style. Preserves code, URLs, file paths, technical terms, and structure. Backs up original as <file>.original.md.
---

<workflow>

1. Verify `$ARGUMENTS` is a non-empty path to an existing file. If missing or empty: `usage: /atomic-compress <path>`. Stop.

2. Refuse if extension is one of `.py .js .ts .tsx .jsx .json .yaml .yml .toml .env .lock .css .scss .html .xml .sql .sh .bash .zsh .rb .go .rs .java .kt .swift .c .cpp .h .hpp`. Message: `refused: code file. compress prose only.` Stop.

3. Refuse if path ends in `.original.md`. Message: `refused: backup file. compress prose only.` Stop.

4. Read the file. Count lines (before).

5. Check if `<path>.original.md` already exists. If yes, ask for confirmation before overwriting. On confirmation: proceed. On refusal: stop. If no: `cp <path> <path>.original.md` (Bash).

6. Compress the prose. Apply rules:

   **Read-only regions** (preserve verbatim):
   - Fenced code blocks (between ` ``` ` markers). Read-only regions.
   - Inline code (backtick spans). Never modify content inside backticks.
   - URLs and Markdown links.
   - File paths (`/src/...`, `./config.yaml`, `~/foo`).
   - Shell commands (`npm install`, `git commit`, `docker build`).
   - Technical terms, library names, API names, protocols, algorithm names.
   - Proper nouns (project names, people, companies).
   - Dates, version numbers, numeric values.
   - Environment variables (`$HOME`, `NODE_ENV`).
   - YAML frontmatter at top of file.

   **Preserve structure:**
   - All Markdown headings — keep heading text EXACT, compress body only.
   - Bullet hierarchy (keep nesting levels).
   - Numbered lists (keep numbering).
   - Tables (compress cell prose, keep structure).

   **Compress prose only:**
   - Drop articles: a/an/the.
   - Drop filler: just/really/basically/actually/simply/essentially/generally.
   - Drop pleasantries: sure/certainly/of course/happy to/I'd recommend.
   - Drop hedging: it might be worth/you could consider/it would be good to/perhaps/maybe.
   - Drop connective fluff: however/furthermore/additionally/in addition.
   - Replace verbose phrasing: "in order to" → "to", "make sure to" → "ensure", "the reason is because" → "because".
   - Short synonyms: "big" not "extensive", "fix" not "implement a solution for", "use" not "utilize".
   - Fragments OK: "Run tests before commit" not "You should always run tests before committing".
   - Drop "you should", "make sure to", "remember to" — state the action.
   - Merge redundant bullets that say the same thing differently.
   - Keep one example where multiple show the same pattern.

   If a heading body contains only critical command details, preserve verbatim.
   If file has no prose (pure code blocks only): state and stop. Do not write.

7. Write compressed content back to the original path.

8. Report: lines before / lines after / percent reduction. Path to backup.

</workflow>

<constraints>

## Rules

- One file per invocation. Multiple paths or globs: ask which to compress.
- `CLAUDE.md` or `README.md`: ask for confirmation before compressing — these are load-bearing.
- Never compress a file that is pure code blocks. State and stop.

</constraints>
