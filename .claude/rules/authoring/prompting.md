---
paths:
  - "agents/**"
  - "templates/agents/**"
  - "skills/**"
  - "commands/**"
  - "templates/commands/**"
  - "templates/shared/**"
  - "output-styles/**"
  - "rules/**"
---

# Claude prompting best practices

Reference for prompt engineering with Claude Opus 4.7 / Sonnet 4.6 / Haiku 4.5. Sourced from Anthropic's official guide. Consult when editing agents, skills, commands, or output styles.

Source: https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices


## Opus 4.7 behavioral notes

- **More literal instruction following.** Interprets prompts more literally than Opus 4.6, especially at lower effort. Will not silently generalize. If you need an instruction applied broadly, state the scope explicitly.
- **Effort parameter matters more.** Respect for effort levels is stricter. `low`/`medium` → scoped work, risk of under-thinking on complex tasks. Use `high` or `xhigh` for intelligence-sensitive work. Start with `xhigh` for coding/agentic use cases.
- **Fewer subagents by default.** Steerable through prompting — give explicit guidance on when subagents are desirable.
- **Less tool use, more reasoning.** Tends to reason over using tools. Increase effort or explicitly instruct tool use to counteract.
- **More direct tone.** Less validation-forward, fewer emoji. Re-evaluate style prompts against the new baseline.
- **Design defaults.** Strong house style (cream backgrounds, serif type, terracotta accents). Override with concrete alternatives or have the model propose options first.


## General principles

### Be clear and direct

Show your prompt to a colleague with minimal context. If they'd be confused, Claude will be too.

- Be specific about desired output format and constraints.
- Provide instructions as sequential steps using numbered lists when order matters.
- If you want thorough behavior, request it explicitly: "Include as many relevant features as possible. Go beyond the basics to create a fully-featured implementation."

### Add context (WHY) to improve performance

Explain motivation behind instructions. Claude generalizes from the explanation.

```
Less effective: NEVER use ellipses
More effective:  Your response will be read aloud by a text-to-speech engine,
                 so never use ellipses since the engine won't know how to
                 pronounce them.
```

### Use examples effectively

3-5 well-crafted examples dramatically improve accuracy and consistency. Make them:

- **Relevant:** mirror actual use case
- **Diverse:** cover edge cases, avoid unintended patterns
- **Structured:** wrap in `<example>` tags (multiple in `<examples>`) so Claude distinguishes them from instructions

### Structure prompts with XML tags

XML tags reduce misinterpretation when prompts mix instructions, context, examples, and variable inputs. Use consistent, descriptive tag names: `<instructions>`, `<context>`, `<input>`, `<constraints>`, `<workflow>`, `<output_format>`.

### Give Claude a role

Even a single sentence in the system prompt focuses behavior and tone.

### Long context prompting

- Put longform data at the top, queries/instructions at the end (up to 30% quality improvement).
- Wrap documents in `<document index="N">` with `<source>` and `<document_content>` subtags.
- Ask Claude to quote relevant parts before answering — cuts through noise.


## Output and formatting

- **Positive framing over negative.** "Write in flowing prose paragraphs" beats "Do not use markdown." Positive examples more effective than negative.
- **Match prompt style to desired output.** Removing markdown from your prompt reduces markdown in output.
- **Use XML format indicators.** "Write prose sections in `<flowing_paragraphs>` tags."
- **LaTeX default.** Claude defaults to LaTeX for math. Add explicit "plain text only" instruction if unwanted.


## Tool use

### Explicit action instructions

Opus 4.7 follows literal instructions. "Can you suggest changes?" → Claude suggests. "Make these changes" → Claude acts. Be explicit about intent.

For proactive tool use by default:

```xml
<default_to_action>
By default, implement changes rather than only suggesting them. If the user's
intent is unclear, infer the most useful likely action and proceed, using tools
to discover any missing details instead of guessing.
</default_to_action>
```

### Parallel tool calling

Steerable to ~100% with explicit prompting:

```xml
<use_parallel_tool_calls>
If you intend to call multiple tools and there are no dependencies between the
tool calls, make all of the independent tool calls in parallel. Never use
placeholders or guess missing parameters in tool calls.
</use_parallel_tool_calls>
```

### Dial back over-prompting

Instructions that were needed for older models ("CRITICAL: You MUST use this tool") may cause overtriggering on Opus 4.7. Use normal prompting: "Use this tool when..."


## Thinking and reasoning

### Adaptive thinking (preferred for Opus 4.7 / Sonnet 4.6)

`thinking: {type: "adaptive"}` — Claude dynamically decides when and how much to think. Calibrated by effort parameter and query complexity.

- **Prefer general instructions over prescriptive steps.** "Think thoroughly" often beats hand-written step-by-step plans.
- **Guide interleaved thinking:**

```
After receiving tool results, carefully reflect on their quality and determine
optimal next steps before proceeding. Use your thinking to plan and iterate
based on this new information, and then take the best next action.
```

- **Steer thinking frequency.** If thinking triggers too often (large system prompts can cause this):

```
Thinking adds latency and should only be used when it will meaningfully
improve answer quality — typically for problems that require multi-step
reasoning. When in doubt, respond directly.
```

- **Self-check.** "Before you finish, verify your answer against [test criteria]." Catches errors reliably for coding and math.
- **Multishot examples with thinking.** Use `<thinking>` tags in few-shot examples — Claude generalizes the reasoning pattern.

### Avoid overthinking

- Replace blanket defaults with targeted instructions: "Use [tool] when it would enhance understanding" instead of "Default to using [tool]."
- Remove over-prompting. Tools that undertriggered in prior models likely trigger appropriately now.
- Use effort as a fallback for overly aggressive behavior.


## Agentic systems

### Long-horizon reasoning

Claude maintains orientation across extended sessions. For multi-context-window workflows:

1. Use the first window for setup (tests, scripts, framework). Subsequent windows iterate on a todo list.
2. Have the model write tests in structured format (e.g. `tests.json`) before starting work.
3. Create setup scripts (`init.sh`) for graceful starts after context refresh.
4. Starting fresh often beats compaction — Claude discovers state from the filesystem.
5. Use git for state tracking across sessions.

### Balancing autonomy and safety

```
Consider the reversibility and potential impact of your actions. Take local,
reversible actions freely (editing files, running tests). For actions that are
hard to reverse, affect shared systems, or could be destructive, ask before
proceeding.
```

### Subagent orchestration

Claude spawns fewer subagents on Opus 4.7. Steerable:

```
Spawn multiple subagents in the same turn when fanning out across items or
reading multiple files. Do not spawn a subagent for work you can complete
directly in a single response.
```

### Minimize hallucinations

```xml
<investigate_before_answering>
Never speculate about code you have not opened. If the user references a
specific file, you MUST read the file before answering. Investigate and read
relevant files BEFORE answering questions about the codebase.
</investigate_before_answering>
```

**For library/framework/API claims:** use `context7` MCP (`resolve-library-id` → `query-docs`) when the user has it installed; fall back to `WebFetch` against official documentation URLs. Training data may lag behind releases — verify even when confident.

### Code review harnesses

Opus 4.7 has higher bug-finding recall but may under-report if filtering instructions are too aggressive. Recommended:

```
Report every issue you find, including ones you are uncertain about or consider
low-severity. Do not filter for importance or confidence at this stage. For
each finding, include your confidence level and estimated severity so a
downstream filter can rank them.
```

### Avoid overengineering

```
Only make changes that are directly requested or clearly necessary:
- Don't add features, refactor code, or make improvements beyond what was asked
- Don't add docstrings, comments, or type annotations to unchanged code
- Don't add error handling for scenarios that can't happen
- Don't create helpers or abstractions for one-time operations
```

### Avoid hard-coding / test-focused solutions

```
Implement a solution that works correctly for all valid inputs, not just the
test cases. Do not hard-code values. Tests verify correctness — they don't
define the solution.
```

### Reduce file creation

```
If you create temporary files, scripts, or helpers for iteration, clean them
up by removing them at the end of the task.
```


## Key patterns for this repo's artifacts

These patterns from the best practices doc directly inform how agents, skills, and commands should be written:

| Pattern | Where to apply | Example |
|---------|---------------|---------|
| XML structural tags | Agent prompts with mixed concerns | `<workflow>`, `<output_format>`, `<constraints>` |
| `<example>` wrapping | Any skill/agent with inline examples | `<examples><example>...</example></examples>` |
| Positive framing | Skill discipline sections | "Write the test first" not "Don't write code before tests" |
| Explicit parallel guidance | Agents that read/verify multiple files | "Read all changed files in parallel" |
| Reflection after tool use | Builder/surgeon workflow | "Reflect on what you found before proceeding" |
| Self-check before completion | Builder/surgeon before reporting done | "Re-read spec success criteria, confirm each is met" |
| "Go beyond" modifiers | Strategist, planner | "Surface non-obvious tradeoffs and second-order effects" |
| Report-everything for review | Reviewer severity tiers | "Always emit nits with confidence level" |
| Context/WHY for instructions | Any behavioral constraint | Explain the motivation, not just the rule |
