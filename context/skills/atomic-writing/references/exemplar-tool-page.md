# Exemplar: the tool reference

The shape of a finished reference page for a tool the reader will drive: a CLI, a daemon, a protocol, an interpreter session. Imitate the slot order; the subject is incidental. The difference from a config reference is the opener: a tool is shown running before it is explained.

## The shape

```text
# <tool>                     one paragraph: what it does, then the constraint line (platforms, scope)
## A worked example          a real transcript of the tool running, and one diagram that compresses it
## The model                 the two or three concepts the verbs operate on
## Verbs                     one section per verb: synopsis, one real invocation, edge cases as a table
## Exit codes                one table, every code paired with its remedy
## State on disk             where files live, what survives what
```

## The slots, with excerpts

**Worked example first.** A reader who watches the tool run holds a model before any reference material lands. Real commands with real output, not placeholders:

```text
atomic repl start --name analysis --lang py
atomic repl eval --name analysis 'import pandas as pd; df = pd.read_csv("data.csv")'
atomic repl eval --name analysis 'df.describe()'
```

> The second `eval` sees `df` because the interpreter never restarted.

One diagram beside the transcript compresses it, so the shape and the transcript reinforce each other: a `sequenceDiagram` when parties exchange messages, a `stateDiagram-v2` when the tool's sessions have a lifecycle.

**The model before the verbs.** Name the concepts the verbs operate on (rooms and members, sessions and scopes) in their own short sections. A verb reference read without the model is a flag list.

**Per-verb sections: synopsis, one real invocation, then the condition table.** The failure modes never lead:

| Condition | Behavior |
|---|---|
| same `--name` already alive | reports already-running; no duplicate spawn |
| interpreter not on `PATH`, no `--bin` given | exit 6 before any spawn, naming the missing binary |

**Exit codes with remedies.** A code without a remedy is trivia; a code with one is a next command:

| Code | Meaning |
|---|---|
| 5 | session dead (socket unreachable) — remedy is `start` |
| 7 | protocol version mismatch — remedy is `stop` then `start`, distinct from 5 |

## Why this shape

A tool page fails when it opens on architecture (the daemon, the socket, the wire format) and makes the reader assemble what using it feels like from flag descriptions. Opening on a transcript inverts that: the reader has driven the tool by the second screen, the model section names what they saw, and the verb sections become lookup instead of first contact.
