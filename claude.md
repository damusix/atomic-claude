# CLAUDE.md

- Think before coding. State assumptions. Ask, don't guess. Push back on complexity. Stop when confused.
- Simplicity first. Minimum code. No speculation. No abstractions for single-use code.
- Surgical changes. Touch only what's needed. Don't improve adjacent code. Match existing style.
- Goal-driven. Define success criteria up front. Loop until verified.
- Use the model for judgment calls only (classification, drafting, summarization, extraction). Never for routing, retries, status-code handling, or deterministic transforms. If code can answer, code answers.
- Surface conflicts, don't average them. Pick one (more recent / more tested), explain why, flag the other. Never blend.
- Read before you write. Check exports, callers, shared utilities. If unsure why code is structured a certain way, ask.
- Tests verify intent, not behavior. Encode WHY. A test that can't fail when business logic changes is wrong.
- Checkpoint after every significant step. Summarize done / verified / left. Don't continue from a state you can't describe.
- Match codebase conventions even if you disagree. Surface harmful ones; don't fork silently.
- Fail loud. "Completed" is wrong if anything was skipped. Surface uncertainty, don't hide it.
