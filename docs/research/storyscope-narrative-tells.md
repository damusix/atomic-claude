# StoryScope: discourse-level AI tells and what transfers to docs


Researched 2026-09-18 and checked against the paper body on 2026-09-19. Sources:
[StoryScope: Investigating idiosyncrasies in AI fiction](https://arxiv.org/abs/2604.03136) (Russell,
Rajendhran, Pham, Iyyer, Wieting; University of Maryland and Google DeepMind; COLM 2026, arXiv v6), its
code at [`jenna-russell/storyscope`](https://github.com/jenna-russell/storyscope), and the 304-feature table
the authors released with it.

This note asks which of the paper's AI-versus-human differences have a form in the docs this repo ships.
The answer became the `Structural tells` checks in `context/skills/atomic-writing/SKILL.md`.


## What the paper claims


AI-written stories separate from human ones on discourse choices, not only on word choice. StoryScope
induces 304 interpretable features across 10 narrative dimensions and applies them to 61,608 stories. Each
of 10,272 prompts has one human story from Books3 and one story from each of five LLMs, less 24
generations the models refused.

A classifier on the narrative features alone, with the 47 style-related features withheld, reaches 93.2%
macro-F1 on human-vs-AI. It reaches 68.4% on six-way attribution. The 30 core features alone reach 84.8%
on human-vs-AI.

The claim that these choices survive a style edit rests on one experiment (§4.2). The authors rewrote 278
Gemini stories with Gemini, using the LAMP span-level editor. LAMP removes seven categories of surface
artifacts, such as cliché and purple prose. Detection fell from 95.5% to 93.9% macro-F1. That is one model
editing its own output, not a human editor.

The dimensions, with the feature count the authors assigned to each and one feature name from the released
taxonomy:

| Dimension | Features | Example feature |
|---|---|---|
| Agents | 54 | Naming practice for the central character |
| Social networks | 39 | Size of social network |
| Style | 39 | Figurative device density |
| Plot | 28 | Thematic resolution pattern |
| Setting | 27 | Dominant spatial scale |
| Events | 26 | Event density per text length |
| Revelation | 25 | Global withholding intensity |
| Situatedness | 25 | Genre fidelity mode |
| Temporal structure | 24 | Dominant pacing mode |
| Perspective | 17 | Dominant narrative person |


## What we reproduced


`data/storyscope_features.parquet` at commit `642e746` of the authors' repo has feature values for 61,575
stories. The 33 missing from the 61,608 are human stories with no feature row.

For each feature we took the share of non-null values at each value, for `source == human` and for the
five AI sources pooled. We then ranked all 304 features by total variation distance. A multi-select feature
counts each joined combination as one value. The scripts are not committed; this method and the pinned
commit reproduce every figure below.

The first table lists the features behind the `Structural tells` checks, plus figurative device density,
which shows where Claude departs from the pool. Most ranks it skips describe characters,
setting, genre, allusion, or plot. Rank 7 is typical: emotional expression through embodied sensation, 81%
against 39%. A few skipped style features could transfer and are not used yet, such as Latinate versus
Anglo-Saxon vocabulary at rank 19.

| Rank | ID | Feature | AI-typical value | AI | Human |
|---|---|---|---|---|---|
| 2 | `STY_FIG_001` | Figurative device density | 4 of 5 | 65% | 18% |
| 4 | `SIT_MET_303` | Thematic explicitness and moralizing | 4 of 5 | 75% | 37% |
| 5 | `STY_FIG_004` | Presence of extended conceit | present | 83% | 40% |
| 9 | `STY_ALL_015` | Lexical register and consistency | consistently elevated | 40% | 11% |
| 10 | `PLT_MOR_007` | Post-climax denouement length | extended | 51% | 15% |
| 12 | `STY_TON_006` | Sound patterning prominence | noticeable | 91% | 55% |
| 14 | `PLT_THM_008` | Thematic unity | 5 of 5 | 74% | 41% |
| 15 | `STY_TON_024` | Rhythmic markedness of prose | 4 of 5 | 61% | 28% |
| 21 | `STY_TON_029` | Information density per sentence | 4 of 5 | 62% | 36% |
| 22 | `STY_FIG_005` | Recurrent metaphorical motif | present | 96% | 69% |
| 27 | `SIT_MET_501` | Narratorial thematic commentary presence | yes | 76% | 52% |
| 47 | `PLT_THM_009` | Integration of subplots with theme | no subplots | 79% | 58% |
| 52 | `REV_SUS_005` | Reader expectation strategy | set up and fulfilled | 73% | 53% |

Four features lean the other way, toward a value human authors choose more often:

| ID | Feature | Human-typical value | AI | Human |
|---|---|---|---|---|
| `STY_ALL_015` | Lexical register and consistency | mixed, frequent code-switching | 19% | 56% |
| `PLT_MOR_007` | Post-climax denouement length | brief | 44% | 73% |
| `STY_FIG_003` | Conventional vs fresh figurative language | mix of cliché and fresh | 35% | 69% |
| `STY_TON_023` | Use of irony and humor | occasional or pervasive | 62% | 88% |

Of the AI stories, 38% are entirely straight-faced, against 12% of human ones.

Output in this repo is written by Claude, so we repeated the comparison per model. Claude shows the same
tells as the pooled AI sources. On every feature in the first table except figurative density, its rate
falls within 10 points of the pooled figure. On figurative density it sits near human authors, at 20%
against 18%. Its widest gap from human authors is the extended denouement, 60% against 15%.

Where Claude departs from the other four models:

| ID | Feature | Claude-typical value | Claude | Human | Other AI |
|---|---|---|---|---|---|
| `STY_CPX_002` | Predominant sentence length band | 21-35 words | 85% | 46% | 41% |
| `STY_CPX_010` | Mean sentence length category | 21-30 words | 55% | 30% | 17% |
| `STY_CPX_011` | Syntactic subordination depth | 4 of 5 | 56% | 29% | 26% |
| `STY_CPX_013` | Parenthetical aside frequency | frequent | 32% | 14% | 6% |
| `EVT_SCH_003` | Strength of event escalation | 3 of 5 | 56% | 30% | 28% |
| `STY_FIG_001` | Figurative device density | 3 of 5 | 77% | 64% | 22% |

Claude has the lowest mean figurative density of the five models, at 3.18. Human authors average 3.00 and
the other models 3.73 to 3.87. Claude also has the highest mean subordination depth of all six sources:
3.57, against 3.27 for human authors. The abstract describes Claude's event escalation as notably flat.


## What transfers to the files this repo ships


The corpus is short fiction averaging 4,753 words, so nothing here is a measurement about READMEs. The
mechanism transfers: these are defaults the model reaches for when producing text, and several have a
direct form in technical prose. Features about characters, relationships, setting, point of view, events,
and time order have no analogue in a reference page.

Figurative density has no row of its own, because Claude already sits near human authors on it.

`atomic-writing` already fixed the page order and kept an avoid list of lexical tells. It had no check for
page-level defaults such as a closing verdict or a recap.


## Decision


Add a page-level layer to `atomic-writing` rather than more words to the avoid list:
a `Structural tells` check list in `context/skills/atomic-writing/SKILL.md`.

Rejected: porting the 30 core features as a checklist. Most of them describe characters, senses, setting,
and time order in fiction. The ones that do transfer are already the closing-line and exception checks.


## What would settle the open question


Whether these checks hold for technical prose is untested. The cheap experiment points the paper's own
pipeline at a parallel corpus built from this repo:

1. Take 100 merged PRs that changed a `docs/` page.
2. Use the human-written page as the reference.
3. Regenerate the page from the diff with the model.
4. Score both with a docs-adapted feature set.
