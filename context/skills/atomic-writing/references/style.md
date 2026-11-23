# Shared writing style

Write for the human who owns the decision, in the same plain, specific voice across documentation and agent instructions. Choose length by the contract, not a compression quota. Keep the grammar and the detail needed to act.

## Sentences

- Name the actor and use active voice. Use literal verbs for inanimate subjects: `returns false`, not “swallows the error.”
- Name the implication, reason, and scope. Replace vague claims and authority crutches such as “everyone” or “always” with the actual case.
- Lead with the answer. Cut throat-clearing, dramatic binary contrasts, reveal formulas, rhetorical prompts, and announcements of importance. For example, replace “Here's the thing: configuring the bundle isn't hard. It's just tedious” with “Configuring the bundle is tedious, not hard.”
- Prefer plain words to marketing and stock AI phrases. Say “handle” rather than “navigate challenges,” “analysis” rather than “deep dive,” and name what breaks instead of “load-bearing.” Reserve “ship” for release or deployment. Delete “it's worth noting” and state the note.
- Remove filler adverbs such as `really`, `just`, `actually`, `inherently`, and `crucially`; retain technical terms such as `asynchronously` and `recursively`.
- Use commas, periods, or parentheses instead of em dashes in prose. An em dash as a structured field separator (`name — responsibility`) stays.
- Keep articles and a useful `that`; keep subject and verb close. Limit noun stacks to two, prefer conventional verbs and common phrases, and preserve identifiers exactly.
- Trust the reader. Cut hand-holding, defensive disclaimers, repeated summaries, and transitions that merely announce the next section. Avoid mechanical three-item rhythms.

## Prose review

Apply these checks to README, guides, designs, research, and reference prose, not specs, wiki pages, or lookup tables:

- Keep a metaphor to one sentence; end on the last fact rather than restating a section or page.
- Restore a material exception if the draft dropped one; do not invent hedges. Name a likely wrong assumption where it matters.
- Vary consecutive sentence openings. Quote a relevant transcript or error when it adds evidence, not artificial variety.
- Split sentences over 30 words and nested asides at clause boundaries. A short parenthetical, or a `, so` or colon join under 30 words, may stay. Do not bridge a split with `therefore`, `thus`, `nonetheless`, or `This means`.

Read twice: first for the substantive checks, then sample three paragraphs for sentence length, repeated openings, and register.
