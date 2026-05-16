# Claude Code: Agent Configuration & System Prompt Guide

A comprehensive guide to configuring agents, subagents, memory, and system prompts in Claude Code.

---

## Table of Contents

1. [Main Agents](#main-agents)
2. [Subagents](#subagents)
3. [Agent Memory System](#agent-memory-system)
4. [System Prompt Modification](#system-prompt-modification)
5. [Output Styles](#output-styles)

---

## Main Agents

### Overview

Main agents are the primary Claude instance you interact with when you launch Claude Code. They define the system prompt, initial behavior, and tools available at startup.

### Creating a Main Agent

Create a markdown file in any of these locations (walked from `cwd` upward to git root, plus user/managed):

- **Project-level** (team-shared, committed): `<repo>/.claude/agents/my-agent.md` (and any parent directory between cwd and the git root)
- **User-level** (personal, global): `~/.claude/agents/my-agent.md`
- **Managed/policy-level** (corporate IT): `<managed-dir>/.claude/agents/*.md`

The agent's identifier (`agentType`) comes from the **`name:` field in frontmatter**, NOT from the filename. The filename is recorded separately for diagnostics. Two files with the same `name:` will collide — later sources override earlier ones (see [Override Order](#override-order)).

### File Structure

```markdown
---
name: "agent-name"
description: "What this agent does"
model: "claude-opus-4-6"
tools: "Bash, Read, Write, Edit"
permissionMode: "default"
effort: "high"
initialPrompt: "Optional setup instructions"
memory: "project"
---

Your system prompt goes here.
This becomes the agent's complete system prompt.
You can write multiple paragraphs.

# Guidelines

- Specific instructions
- Constraints
- Behavior expectations
```

### Frontmatter Fields

| Field | Type | Required? | Notes |
|-------|------|-----------|-------|
| `name` | string | ✓ | Agent identifier (`agentType`); independent of filename |
| `description` | string | ✓ | Short description; shown in agent listings; supports `\n` for newlines |
| `model` | string | ✗ | Model ID (e.g., `"claude-opus-4-6"`) or `"inherit"`; defaults to session model |
| `tools` | string or array | ✗ | Available tools; missing = all tools; `"*"` = all tools; empty string/array = no tools. Separators: comma OR space (e.g., `"Bash, Read"` or `"Bash Read"`) |
| `disallowedTools` | string or array | ✗ | Tools to exclude from available set (same format as `tools`) |
| `permissionMode` | string | ✗ | One of: `"default"`, `"acceptEdits"`, `"plan"`, `"bypassPermissions"`, `"dontAsk"` (and internally `"auto"`, gated by `TRANSCRIPT_CLASSIFIER` feature flag) |
| `effort` | string or int | ✗ | One of `"low"`, `"medium"`, `"high"`, `"max"`, or a positive integer. Effort is also gated by per-model support (`modelSupportsEffort`) |
| `initialPrompt` | string | ✗ | **Main agent only.** Prepended to first user turn; slash commands are processed |
| `memory` | string | ✗ | Memory scope: `"user"`, `"project"`, or `"local"`. Only applied if `autoMemoryEnabled` is on |
| `skills` | string or array | ✗ | Skill names to preload (e.g., `"commit, review"`) |
| `mcpServers` | array | ✗ | MCP server specs: strings (refs to existing servers by name) or `{ name: config }` inline definitions |
| `hooks` | object | ✗ | Session-scoped hooks (see hooks documentation) |
| `color` | string | ✗ | Agent color for UI; must be one of the values in `AGENT_COLORS` |
| `maxTurns` | integer | ✗ | Max agentic turns before stopping (positive int) |
| `background` | boolean or string | ✗ | Run as background task by default |
| `isolation` | string | ✗ | `"worktree"` (or `"remote"` for Anthropic-internal builds) |
| `requiredMcpServers` | array | ✗ | MCP server name patterns that must be configured for agent to be available |

### Example: Code Review Agent

```markdown
---
name: "code-reviewer"
description: "Specialized code review agent with security and performance focus"
model: "claude-opus-4-6"
tools: "Bash, Read, Edit, Grep"
initialPrompt: "You are in code review mode. Focus on security, performance, and maintainability."
memory: "project"
effort: "high"
---

You are an expert code reviewer with deep expertise in:

- Security vulnerabilities (OWASP top 10, injection attacks, authentication)
- Performance bottlenecks (algorithms, queries, memory)
- Maintainability (readability, testing, documentation)
- Best practices (design patterns, error handling)

## Review Process

When reviewing code:

1. **Read** the complete file to understand context
2. **Identify** specific issues with line numbers
3. **Suggest** alternatives with explanations
4. **Prioritize** by severity: security > performance > style

## Output Format

For each issue found:
- **Line 42**: Security issue—SQL injection risk. Suggestion: Use parameterized queries.
- **Lines 100-110**: Performance—O(n²) loop. Suggestion: Use Set instead of array.

Be constructive but thorough. Users value specific, actionable feedback.
```

### Using a Main Agent

Launch Claude Code with a specific agent:

```bash
# Use the code-reviewer agent
claude --agent code-reviewer

# List available agents (in UI or help)
claude --help
```

### Agent Discovery

Claude Code aggregates agents from these sources:

- Built-in agents (e.g., `general-purpose`, `Explore`, `Plan`)
- Plugin agents (from enabled plugins)
- User agents (`~/.claude/agents/*.md`, source = `userSettings`)
- Project agents (`<repo>/.claude/agents/*.md` walked from cwd up to git root, source = `projectSettings`)
- CLI-flag agents (`--agents <json>`, source = `flagSettings`)
- Managed/policy agents (e.g., `/etc/claude-code/.claude/agents/*.md`, source = `policySettings`)

#### Override Order

When two agents share the same `name`, later groups overwrite earlier ones. The order applied in `getActiveAgentsFromList()` is:

1. `built-in` (lowest priority — overwritten by everything below)
2. `plugin`
3. `userSettings` (`~/.claude/agents/`)
4. `projectSettings` (`<repo>/.claude/agents/`)
5. `flagSettings` (`--agents` CLI flag)
6. `policySettings` (managed / corporate IT — **highest priority**)

So: managed > CLI flag > project > user > plugin > built-in.

---

## Subagents

### Overview

Subagents are specialized agents spawned by the main agent to parallelize work or handle specific tasks. They receive a fresh context with only the information passed to them.

### Key Difference: No initialPrompt

Unlike main agents, **subagents do NOT use the `initialPrompt` field**. The prompt is passed directly as the `prompt` parameter in the AgentTool call.

### Creating a Subagent

Create a markdown file with the same structure as a main agent (any agent definition can be used as a subagent — it's the same file format, the same loader, the same `agentType`):

```markdown
---
name: "security-auditor"
description: "Deep security analysis of code"
tools: "Bash, Read, Grep"
memory: "project"
---

You are a security expert specializing in vulnerability detection.

Your job: Analyze code for security issues, prioritized by severity.

## Scope

- Input validation
- Authentication/authorization
- Cryptography
- Data exposure
- Injection attacks

## Output

List all findings with file paths, line numbers, risk level, and remediation.
```

### Spawning a Subagent

From the main agent, use the AgentTool:

```
Use the agent tool with:
- subagent_type: "security-auditor"
- prompt: "Audit src/auth/ for security vulnerabilities. Focus on token handling and session management."
```

Or programmatically (from SDK):

```typescript
await agentTool.call({
  subagent_type: "security-auditor",
  prompt: "Analyze src/auth/ for security issues."
}, toolUseContext);
```

### Subagent Context Isolation

Each subagent receives:

- ✓ Its own system prompt (from agent definition's markdown content)
- ✓ Environment details appended via `enhanceSystemPromptWithEnvDetails` (notes about absolute paths, env info, optional DiscoverSkills guidance)
- ✓ The `prompt` parameter you pass (as the first user message)
- ✓ Tools specified in its `tools:` field (plus auto-injected file tools when `memory` is set)
- ✓ Memory section appended to system prompt (if `memory` set and `autoMemoryEnabled`)
- ✓ The base user/system context (CLAUDE.md, etc.) — though `Explore`/`Plan` and any agent with `omitClaudeMd` get a slimmed version
- ✗ NO output style section
- ✗ NO `initialPrompt` from agent frontmatter (that field only fires on the main thread agent)
- ✗ NO main agent's conversation history (unless this is a fork — see below)

**Briefing the Subagent**

Since subagents start fresh, you must provide complete context in the `prompt`:

**Bad** (too vague):
```
prompt: "Investigate the auth bug"
```

**Good** (complete briefing):
```
prompt: "Users report 2FA failing intermittently. 
Investigate src/auth/two-factor.ts:

1. Check token generation logic
2. Verify expiry windows (should be 5 minutes)
3. Look for race conditions in validation
4. Check database queries for deadlocks

Context: System uses TOTP with 6-digit codes. 
Codes are stored in Redis with TTL. Focus on timing issues."
```

### Subagent Forking

If the fork feature is enabled (`isForkSubagentEnabled()`), omitting `subagent_type` forks yourself:

```
Use the agent tool with:
- prompt: "Research the codebase in parallel"
- [omit subagent_type]
```

A fork inherits the parent's conversation context and the parent's exact tool set (for prompt-cache sharing). Recursive forks are blocked at call time.

When the fork feature is OFF, omitting `subagent_type` defaults to the `general-purpose` agent.

---

## Agent Memory System

### Overview

Agent memory is persistent, per-agent storage that survives across sessions. It allows agents and subagents to learn and accumulate knowledge about projects and patterns.

### Memory Scopes

Enable memory by adding `memory:` to agent frontmatter:

```markdown
---
name: "my-agent"
memory: "project"
---
```

Three scopes are available:

| Scope | Location | Shared? | Use Case |
|-------|----------|---------|----------|
| `user` | `~/.claude/agent-memory/{agent-name}/` | Global, all projects | General learnings about agent behavior |
| `project` | `.claude/agent-memory/{agent-name}/` | Team-shared via git | Project-specific architecture, patterns, decisions |
| `local` | `.claude/agent-memory-local/{agent-name}/` | Machine-specific, gitignored | Local machine context |

### Memory Directory Structure

For an agent named `code-reviewer` with `memory: "project"`:

```
.claude/agent-memory/code-reviewer/
├── MEMORY.md              # Index file (200 lines max, ~25KB)
├── security_patterns.md   # Topic file
├── performance_tips.md    # Topic file
├── common_mistakes.md     # Topic file
└── [other topic files]
```

### MEMORY.md (Index File)

This is a pointer index—NOT the content storage:

```markdown
# Persistent Agent Memory

- [JWT implementation details](jwt_details.md)
- [Common SQL injection patterns in codebase](sql_injection_patterns.md)
- [Performance bottlenecks identified](perf_bottlenecks.md)
- [Team coding standards](coding_standards.md)
```

**Limits:**
- Max 200 lines
- Max ~25,000 bytes
- Keep entries under ~150 characters

### Topic Files

Individual memory files with metadata:

```markdown
---
name: JWT Implementation Details
type: project
description: How JWTs are generated and validated in this codebase
---

## Token Generation

Location: `src/auth/token.ts:generateToken()`

- Algorithm: HS256
- Secret: `AUTH_SECRET` env var (32 bytes min)
- Expiry: 24 hours
- Payload: `{ userId, email, roles }`

## Validation

Location: `src/auth/token.ts:validateToken()`

- Checks signature against AUTH_SECRET
- Validates expiry
- Returns 401 if invalid

## Known Issues

- No token rotation on role change (TODO: add)
- Sessions don't auto-logout on secret rotation
```

### How Agents Access Memory

When an agent spawns with `memory: "project"`:

1. **Memory is automatically loaded** into the system prompt as:
   ```
   # Persistent Agent Memory
   
   ## MEMORY.md
   
   [Contents of MEMORY.md]
   ```

2. **File tools are auto-injected**: The agent gets `file-read`, `file-write`, `file-edit` automatically (even if not listed in `tools:`)

3. **Agent can read and update memory** during execution

### Saving Memories

The agent writes to memory in two steps:

**Step 1: Create a topic file**

```
Use the file write tool to create:
File path: .claude/agent-memory/code-reviewer/jwt_details.md

---
name: JWT Implementation Details
type: project
---

[Memory content]
```

**Step 2: Update the index**

```
Use the file edit tool on: .claude/agent-memory/code-reviewer/MEMORY.md

Replace this:
# Persistent Agent Memory

With this:
# Persistent Agent Memory

- [JWT Implementation Details](jwt_details.md)
```

### Memory Works with Both Main and Subagents

Both main agents and subagents can:

- **Read** memory from their MEMORY.md
- **Update** memory by writing topic files
- **Build on** previous sessions' learnings

**Example workflow:**

1. **First run:** Researcher subagent explores auth code, saves learnings
2. **Second run:** Same researcher subagent spawned—reads previous findings
3. **Third run:** Main agent (also with memory) can optionally read researcher's findings

### Memory Types (Taxonomy)

The system encourages organizing memories by type:

```markdown
---
type: "project"     # Project-specific information
---
```

**Valid types:**

| Type | Description |
|------|-------------|
| `project` | Codebase architecture, decisions, patterns |
| `user` | User preferences, workflows, habits |
| `feedback` | Corrections, what to avoid |
| `reference` | Links, external docs, external systems |

### Disabling Memory

`isAutoMemoryEnabled()` gates the entire memory pipeline. It returns `false` (memory off) when ANY of these apply:

- `CLAUDE_CODE_DISABLE_AUTO_MEMORY` env var is truthy
- `CLAUDE_CODE_SIMPLE` (i.e., `--bare`) is set
- `CLAUDE_CODE_REMOTE` is set without `CLAUDE_CODE_REMOTE_MEMORY_DIR`
- `autoMemoryEnabled: false` in `settings.json`

Default is enabled. When disabled, the memory section is NOT appended to the agent's system prompt and the file tools are NOT auto-injected for memory access (the agent only gets what's listed in `tools:`).

---

## System Prompt Modification

### Three Ways to Modify System Prompts

#### 1. Agent Definition (System Prompt Content)

The main content of your agent markdown file IS the system prompt:

```markdown
---
name: "my-agent"
---

# You are a Python expert
# This entire section becomes the system prompt

Your role: Review Python code for style and efficiency.

Rules:
- Enforce PEP 8
- Suggest type hints
- Flag O(n²) operations
```

**Where it applies:**
- ✓ Main agent (entire prompt)
- ✓ Subagents (entire prompt)
- ✓ Both use the markdown content as-is

**How it's used:**
```
System Prompt = [agent markdown content]
                + [memory prompt, if memory enabled]
                + [environment details, language prefs, etc.]
```

#### 2. Subagent-Only: Direct Prompt Parameter

When spawning a subagent, the `prompt` parameter becomes the first user message—NOT system prompt modification:

```
Use agent tool:
- subagent_type: "code-reviewer"
- prompt: "Review this for performance issues"  # ← Goes as user message, not system prompt
```

**Important distinction:**
- System prompt = agent's instructions (from agent definition)
- Prompt parameter = specific task for this invocation

#### 3. Output Styles (Main Agent Only)

Output styles affect ONLY the main agent's system prompt, NOT subagents.

**Where it appears in system prompt:**

```
# Output Style: Explanatory

[Output style instructions here]
```

This section is:
- ✓ Injected into main agent's system prompt
- ✗ NOT injected into subagent system prompts
- ✗ NOT available via agent definition
- ✗ NOT modifiable via agent or subagent config

See [Output Styles](#output-styles) section below for details.

---

## Output Styles

### Overview

Output styles define how Claude responds—verbosity level, explanation depth, format preferences, etc. They affect ONLY the main agent, not subagents.

### Key Facts About Output Styles

✓ **Main agent only** — Subagents do NOT receive output style instructions  
✓ **User preference** — Set via settings, not agent definition  
✗ **Not in agent config** — Cannot be specified in `.claude/agents/*.md`  
✗ **Not per-subagent** — Subagents follow their own system prompt only  

### How Output Styles Work

1. **User sets output style** via CLI or settings
2. **Output style loaded** at session start
3. **"Output Style" section appended** to main agent's system prompt:
   ```
   # Output Style: [StyleName]
   
   [Style-specific instructions]
   ```
4. **Subagents never see this section**

### System Prompt Composition

**Main Agent:**
```
[Agent definition content]
[Language section if set]
[Output Style section]  ← Main agent only
[Memory section if enabled]
[Environment details]
[Dynamic sections]
```

**Subagent:**
```
[Subagent definition content]
[Memory section if enabled]
[Environment details - no output style]
[Dynamic sections - no output style]
```

### Built-in Output Styles

Claude Code ships with these built-in styles (defined in `src/constants/outputStyles.ts`):

- **`default`** — No extra prompt section appended (standard behavior)
- **`Explanatory`** — Adds educational "Insight" callouts about implementation choices
- **`Learning`** — Pauses to ask the user to write small (2-10 line) code snippets for hands-on practice

Plugins can also contribute output styles, and a plugin style can `forceForPlugin: true` to auto-apply when its plugin is enabled.

### Customizing Output Styles

Custom output styles are markdown files (much like agents) loaded from:

- `~/.claude/output-styles/*.md` (user-level)
- `<repo>/.claude/output-styles/*.md` (project-level, overrides user)
- Managed (`policySettings`) directories (highest priority)

The priority order, applied in `getAllOutputStyles()`, is: built-in → plugin → user → project → managed.

Select an output style with the `/output-style` slash command, or set `outputStyle` in `settings.json`. The selection is stored in user/project settings; the actual prompt content lives in the markdown files above.

### Why Subagents Don't Get Output Styles

**Design rationale:**
- Output styles are user preferences for interactive terminals
- Subagents are task-specific workers (non-interactive)
- Output style would interfere with specialized agent instructions
- Subagents should follow their own system prompt (agent definition)

**Example:**

If main agent has output style "Brief" and spawns a researcher subagent:
- **Main agent** output is brief and concise
- **Subagent** follows its own system prompt (may produce detailed findings)
- Main agent's "Brief" style does NOT constrain subagent's output

---

## Complete Example: Multi-Agent Setup

### Project Structure

```
my-project/
├── .claude/
│   └── agents/
│       ├── researcher.md
│       ├── code-reviewer.md
│       └── tech-lead.md
├── src/
└── [project files]
```

### Agent Definitions

**`.claude/agents/tech-lead.md`** (main agent):

```markdown
---
name: "tech-lead"
description: "Technical lead coordinating code review and research"
model: "claude-opus-4-6"
tools: "*"
initialPrompt: "You are the tech lead. Coordinate with specialists for code reviews and research."
memory: "project"
effort: "high"
---

You are the technical lead for this project. Your role:

1. **Understand** what the user needs
2. **Coordinate** with specialist agents (researcher, code-reviewer)
3. **Synthesize** findings into actionable recommendations
4. **Document** decisions and learnings for the team

## Workflow

When a complex task arrives:

1. Spawn researcher for deep codebase investigation
2. Spawn code-reviewer for architectural assessment
3. Combine findings and synthesize recommendations
4. Document key learnings in MEMORY.md

## Constraints

- Don't perform code review directly; delegate to specialist
- Don't research broadly; delegate and wait for results
- Synthesis and decision-making are your domain
```

**`.claude/agents/researcher.md`** (subagent):

```markdown
---
name: "researcher"
description: "Deep codebase researcher"
tools: "Bash, Read, Grep, Glob"
memory: "project"
---

You are a meticulous code researcher. Your job: deeply investigate questions
and synthesize findings into clear, evidence-based reports.

## Research Process

1. **Search** broadly for relevant files and patterns
2. **Read** key files completely for context
3. **Map** relationships between modules
4. **Document** findings with file paths and line numbers
5. **Summarize** in clear, hierarchical format

## Output Format

```
# Findings

## Topic 1
- File: src/auth/token.ts:45
  Description: JWT generation logic
  Pattern: HS256 with env secret

## Topic 2
- File: src/db/pool.ts:20
  Description: Connection pooling
  Limit: Max 10 concurrent connections
```

Save learnings to your memory for next session.
```

**`.claude/agents/code-reviewer.md`** (subagent):

```markdown
---
name: "code-reviewer"
description: "Thorough code review specialist"
tools: "Bash, Read, Edit"
effort: "high"
memory: "project"
---

You are an expert code reviewer. Your job: identify issues and suggest improvements.

## Review Criteria

1. **Security** - Vulnerabilities, data exposure, auth issues
2. **Performance** - Algorithms, queries, memory leaks
3. **Maintainability** - Readability, testing, documentation
4. **Best practices** - Design patterns, error handling, logging

## Output Format

For each issue:
```
**Line 42 - [SECURITY]** SQL Injection Risk
Current: `SELECT * FROM users WHERE id = ${id}`
Suggested: Use parameterized queries: `SELECT * FROM users WHERE id = $1`
Severity: High
```

Save patterns and common issues to memory.
```

### Using the Setup

**Launch as tech lead:**
```bash
claude --agent tech-lead
```

**Invoke from main agent:**
```
I need to understand authentication flow and review the auth module.

Use the agent tool to spawn the researcher:
- subagent_type: "researcher"
- prompt: "Map out the authentication flow. Focus on JWT generation, validation, 
  and token refresh. Document all files involved with line numbers."

After researcher returns, use the agent tool to spawn code-reviewer:
- subagent_type: "code-reviewer"
- prompt: "Review the authentication module (src/auth/) for security issues.
  Focus on token handling, input validation, and error handling. Use the findings
  from the researcher to contextualize your review."

Then synthesize both reports into recommendations.
```

---

## Advanced Topics

### Memory with Multiple Scopes

An agent can only use ONE scope at a time:

```markdown
---
memory: "project"  # Only this scope is loaded
---
```

**Typical patterns:**

- **Personal learning agent** → `memory: "user"` (shared across projects)
- **Project specialist** → `memory: "project"` (team-shared findings)
- **Local experiment** → `memory: "local"` (machine-specific context)

### Plugin Agents

Agents can come from plugins. Plugin agents are namespaced:

```markdown
---
name: "my-plugin:my-agent"  # Plugin-provided agent
---
```

Launch with:
```bash
claude --agent my-plugin:my-agent
```

### MCP Servers in Agent Config

Agents can define their own MCP servers:

```markdown
---
name: "my-agent"
mcpServers:
  - slack        # Reference existing MCP server
  - name: "custom"
    command: "node"
    args: ["./my-mcp-server.js"]
---
```

### Conditional Tool Access

Restrict which tools an agent can use:

```markdown
---
tools: "Bash, Read, Write"           # Only these tools
disallowedTools: "Bash"              # Except Bash
---
```

Final available tools: `Read, Write`

Tool name reference (PascalCase, as registered in tool constants):

- `Bash` — shell execution
- `Read` — file reading
- `Write` — file creation/overwrite
- `Edit` — file editing
- `Grep` — content search
- `Glob` — file pattern matching
- `WebFetch`, `WebSearch`
- `Agent` — spawn subagents
- `TaskCreate`, `TaskUpdate`, `TaskGet`, `TaskStop`
- `NotebookEdit`
- `AskUserQuestion`
- `EnterPlanMode`, `ExitPlanMode`
- `SendMessage`, `TeamCreate`
- `REPL`, `CronCreate`, `CronList`, `CronDelete`
- (plus MCP tools, named per server/tool)

---

## Troubleshooting

### Agent Not Found

**Problem:** `Agent "my-agent" not found`

**Solution:**
1. Verify file exists: `.claude/agents/my-agent.md` or `~/.claude/agents/my-agent.md`
2. Verify the `name:` field in frontmatter is exactly `my-agent` (filename does NOT need to match — `name:` is the only identifier that counts)
3. Verify `description:` is present and non-empty (required field; missing description causes silent skip)
4. Check for parse errors in debug logs (frontmatter must be valid YAML)

### Memory Not Loading

**Problem:** Agent doesn't see its memory despite having `memory: "project"`

**Solution:**
1. Check `autoMemoryEnabled` in settings (may be disabled globally)
2. Verify `.claude/agent-memory/my-agent/MEMORY.md` exists
3. Check permissions on memory directory

### Subagent Not Receiving Tools

**Problem:** Subagent can't use a tool I specified

**Solution:**
1. Check agent definition has tool (PascalCase, e.g., `Bash` not `bash`) in `tools:` field
2. If `memory:` is set, verify `Read`, `Write`, `Edit` are auto-injected (only happens when `autoMemoryEnabled` is on)
3. Check `disallowedTools` isn't excluding it
4. Check permission mode (may prompt or block at call time even when listed)

### Output Style Not Appearing in Main Agent

**Problem:** Output style text not in system prompt

**Solution:**
1. Verify output style is selected in settings
2. Output style only affects main agent—check you're not inspecting subagent prompt
3. Output styles are terminal/UI-specific; may not appear in API responses

---

## Summary Table

| Feature | Main Agent | Subagent | How to Configure |
|---------|-----------|----------|------------------|
| System Prompt | ✓ | ✓ | Markdown content in agent file |
| Memory | ✓ | ✓ | `memory: "user"/"project"/"local"` |
| Tools | ✓ | ✓ | `tools:` field in frontmatter |
| Output Style | ✓ | ✗ | Settings/preferences (not agent config) |
| initialPrompt | ✓ | ✗ | `initialPrompt:` field (main only) |
| Model Override | ✓ | ✓ | `model:` field |
| Permission Mode | ✓ | ✓ | `permissionMode:` field |
| MCP Servers | ✓ | ✓ | `mcpServers:` array |

---

## References

- **Codebase:** `/src/tools/AgentTool/`
- **Agent Loader:** `loadAgentsDir.ts` (agent definition parsing)
- **Memory System:** `src/memdir/` (memory management)
- **Prompt Building:** `src/constants/prompts.ts` (system prompt composition)
- **Output Styles:** `src/constants/outputStyles.ts` (style definitions)
