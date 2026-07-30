# Code-intel MCP setup

`atomic code mcp` runs an MCP server that exposes the code-intelligence graph as tools for the interactive Claude Code session. This lets you ask questions like "what calls this function?" or "what would break if I change this?" in natural language, and Claude resolves them from the real symbol graph instead of grepping.

**This is fully opt-in.** Atomic does not auto-register the MCP server. Subagents (`atomic-investigator`, `atomic-implementer`, `atomic-reviewer`, `atomic-wiki-inferrer`) shell out to `atomic code …` directly and need no MCP registration. MCP is a convenience for the *interactive* session only.

**Try `explore` from the CLI first.** Before registering anything, run `atomic code explore "<question>"` directly after indexing. It takes a natural-language query and returns a bundled digest of the relevant symbols, files, and relationships in one call. This is the fastest way to orient in an unfamiliar codebase and the verb most agents reach for first, and it works immediately from the CLI. MCP registration adds the same graph to the interactive session as tools; the CLI verb needs no registration at all.


## Prerequisites

Index your project first. From your project root:

```bash
atomic code index
```

This creates `.claude/.atomic-index/atomic.db` and adds it to `.gitignore`. The index is project-scoped and never committed.

After the initial index, keep it fresh with:

```bash
atomic code sync
```

`/refresh-wiki` automatically runs `atomic code sync` when the index is warm.


## Register the MCP server

Create or update `.mcp.json`. Use `atomic --repo <abs-path> code mcp` for each repo you want to serve — this is cwd-independent, so the server works correctly from a realm root, a different directory, or any non-git location.

**Single repo:**

```json
{
  "mcpServers": {
    "atomic-code": {
      "command": "atomic",
      "args": ["--repo", "/absolute/path/to/your/project", "code", "mcp"],
      "type": "stdio"
    }
  }
}
```

**Multiple repos (realm or workspace):**

```json
{
  "mcpServers": {
    "atomic-code-server": {
      "command": "atomic",
      "args": ["--repo", "/path/to/server", "code", "mcp"],
      "type": "stdio"
    },
    "atomic-code-gui": {
      "command": "atomic",
      "args": ["--repo", "/path/to/gui", "code", "mcp"],
      "type": "stdio"
    },
    "atomic-code-apps": {
      "command": "atomic",
      "args": ["--repo", "/path/to/apps", "code", "mcp"],
      "type": "stdio"
    }
  }
}
```

Each entry starts an independent daemon. Daemons for different repos never collide — sockets and locks live next to each repo's db file. For realm members (`<realm>/server`), the db resolves to `<realm>/.atomic/server.db` automatically; nothing is written into the member's source tree.

Restart Claude Code after saving `.mcp.json`. Each server starts on demand when Claude first uses a code-intel tool for that repo.

The daemon keeps the graph fresh automatically — it re-syncs every 10 seconds in the background. Pass `--no-watch` to disable background sync, or `--watch-interval <dur>` (e.g. `30s`) to override the interval.


## Available tools

Once registered, Claude has access to these tools in the interactive session:

| Tool | What it answers |
|------|----------------|
| `atomic_code_search` | Find symbols by name, kind, or language |
| `atomic_code_callers` | What calls this symbol? (up to N hops) |
| `atomic_code_callees` | What does this symbol call? (up to N hops) |
| `atomic_code_impact` | What is the blast radius of changing this? |
| `atomic_code_node` | Show detailed info for a symbol |
| `atomic_code_files` | List all indexed files |
| `atomic_code_status` | Show index health and statistics |
| `atomic_code_explore` | Gather context for a natural-language query |

On small repos (fewer than 500 indexed files), only `atomic_code_explore`, `atomic_code_search`, and `atomic_code_node` are registered — the graph-traversal tools add noise when the graph is tiny enough to grep quickly.


## What subagents do instead

Subagents do not use MCP. They shell out to `atomic code` directly:

```bash
atomic code callers MyFunction --json
atomic code impact MyFunction --depth 2 --json
atomic code search "UserService" --json
```

The `agent-code-intel` partial instructs each subagent when and how to use these verbs, with a degradation contract: binary absent, no DB, or failed query → fall back to `sg`/`grep`. Investigator, reviewer, and signals-inferrer compose it directly; the implementer (feature and surgical modes) receives it transitively through `agent-implementer-workflow`.


## Degradation

The MCP server does not require a pre-built index. If `.claude/.atomic-index/atomic.db` does not exist, the daemon initializes an empty index on first run rather than erroring. An empty graph answers nothing useful, though, so build the index first with `atomic code index` and keep it warm with `atomic code sync`.

`atomic doctor` check 11 (`code-index`) reports index health:

- No index → PASS (informational — indexing is opt-in)
- Index present, stale → WARN
- Index present, fresh → PASS

The doctor check never emits FAIL — absence of the index is not an error condition.
