# AI language processing cost and what it changes in atomic's prompts


Researched 2026-11-01. Source: [Why is AI-generated language so hard to understand?](https://arxiv.org/html/2610.29162v1)
(Clark, Martin, Mahowald; New York University and The University of Texas at Austin; arXiv v1, 21 Oct 2026).
We read the body and appendices but not the bibliography.

This note asks which findings apply to the prompts that this repo ships. The answer changed
`context/output-styles/atomic.md` and added rule 18 to `context/skills/atomic-writing/SKILL.md`.


## What the paper claims


Frontier and near-frontier models overuse sentence constructions that psycholinguistic studies associate with
processing difficulty. The authors collected output from 14 models: 12 in chat, 9 in coding, and all 14 in
essays. They compared it to human corpora and to paraphrases made by round-trip translation through Japanese.
A construction that becomes less frequent in the paraphrase is one that the model chose over an available
alternative.

| Construction | Example from the paper | Reported difficulty | Usual shorter-than |
|---|---|---|---|
| Rare word pairs | "reader-patience budget" | Hard-to-predict words take longer to read | The common phrasing |
| Object relative without `that` | "the old code never reached" | Harder than a subject relative; animacy and frequency reduce the gap | The form with `that`, or a passive |
| Stacked nouns (three or more) | "Summer park district classes" | Bracketing can be ambiguous | A phrase with prepositions |
| Ad-hoc nouns used as verbs | "calendar it" | Part-of-speech conversion triggers syntactic re-evaluation; conventional uses are unmarked | "put the whiskey in a bottle" for "bottle the whiskey" |
| Inanimate subjects with human verbs | "this asymmetry bites" | The mismatch may trigger reanalysis when context is missing | "causes an issue" |
| Long dependencies | "The guitar sitting in the corner because you're still "deciding" teaches you zero chords." | Strains working memory | Not always shorter |

In a second experiment, the authors fine-tuned a small model (Qwen2.5-1.5B) under a per-token length penalty.
Both members of each alternation counted as correct, so only length separated them. Without regularization, any
penalty above zero moved all seeds to a near-categorical preference for the short form. The preference for
dropping `that` carried over to constructions outside training. Human writers choose between these forms by
context, and include `that` more often before less predictable material.

Changes under round-trip translation were smaller in essays than in chat and coding on four categories. Claude
Opus 5.5 was lower than Opus 5 on 19 of 21 comparisons, with non-overlapping 95% intervals in eight, so the
overuse varies between model versions.


## How strong the evidence is


| Claim | Status |
|---|---|
| Models overuse these constructions relative to paraphrases | Measured for most models and features. In essays, dependency length is unchanged or slightly longer after the round trip, and stacked nouns change significantly for only two models. Llama and Olmo base show little effect |
| Models overuse them relative to human text | Inanimate subjects and low-frequency n-grams hold for all nine frontier models. Longer dependencies, stacked nouns, and reduced object relatives hold for most, in conversation. Denominal verbs, inanimate subjects, and n-grams have no human baseline in chat or coding, so the n-gram result rests on essays |
| The constructions slow human reading | Prior psycholinguistics, not retested. The paper ran no comprehension study on this corpus |
| A length penalty shifts syntax | Shown on one small model and one task. Far transfer appeared in one of four alternations |
| Verifiable-reward training causes the overuse in frontier models | The authors' hypothesis |
| Turn count explains the gap between essays and chat or coding | The authors' hypothesis. The settings also differ in genre and prompt |

The study covers English only, with 15 conversation seeds.


## What this changes here


We left `agent-atomic-voice` unchanged. Some of the paper's costs come from human memory limits, and that
partial's reader is a model, so this is a judgment call.

Our output style instructed the model to drop articles and prefer short synonyms. That instruction resembles the
length penalty, but it is an extrapolation: the paper tests a training penalty, not a prompt.

| Before | After | Basis |
|---|---|---|
| Drop articles where the noun is predictable | Keep articles | Extrapolation. The paper did not measure articles, and a dropped article before a singular count noun is ungrammatical |
| No rule; the style file's own prose omitted `that` | Keep `that` before hard-to-predict words | Human usage in the paper. A categorical preference for the short form is the divergence that the authors describe |
| Short synonyms | Common words | Extrapolation. The paper measures rare sequences, not word length |
| Fragments OK everywhere | Fragments in labels, list items, and status lines | Judgment. The paper did not measure fragments |
| No syntax rules | A `Keep parseable` table | Names the constructions with a rewrite for each |
| Five Auto-Clarity triggers | Four | With full sentences as the default, the trigger for compression-caused ambiguity has no separate case |

`atomic-writing` rule 2 already banned inanimate subjects with human verbs, but its question about reading order
was "What will bite me?", which is the pattern in the paper's example. The question is now "What can go wrong?".


## What is not settled


The 120-word default stays, and the style now says that grammar wins when they collide. We kept the default on
judgment: the paper does not test how reply length, tables, or diagrams affect reading, and the only length mechanism
it tests is a penalty on total tokens, the nearest analogue of a word cap.

Whether the `Keep parseable` table reduces these constructions in atomic's replies is unmeasured. The paper's
spaCy parse could score dependency length, stacked nouns, and object relatives in transcripts from before and
after this change. Denominal verbs and inanimate subjects also need the paper's external classifier.
