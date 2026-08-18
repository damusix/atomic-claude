# Exemplar: the reference page

The shape of a finished reference page for a config file, a format, or a subsystem. Imitate the slot order and what each slot carries; the subject is incidental. Read this before writing a `docs/reference/` page whose reader arrives to look something up.

## The shape

```text
# <name>                     one paragraph: what it is, who it applies to, what it is not
<the mental model>           one diagram when the model has a shape; the caption states the claim
## Keys | Fields | Parts     the full inventory as one table: name, type, default, one-line what
## <item>                    one section per inventory item, in the inventory's order
## How it is read            the degradation contract as a table: absence, bad input, partial state
```

## The slots, with excerpts

**Opening: the mental model in one screen.** The first paragraph settles what the thing is and who it applies to; the diagram makes the one claim the rest of the page assumes. From a config-file page:

> The repo-scoped config file. It is committed, so it applies to everyone working in the repository, and it holds settings that belong to the project rather than to you.
>
> *(diagram: the personal config and the repo config feed the same binary and do not overlap)*
>
> Your personal preferences live in `~/.atomic/config.toml` and never reach the repository. Facts about the project live here.

**Inventory: lookup without reading.** One table row per item lets the reader find their key and jump; the sections that follow keep the table's order so the jump lands.

| Key | Type | Default | What it does |
|-----|------|---------|--------------|
| `scope` | string | none | Declares this directory as `"repo"` or `"realm"` |
| `[code] ignore` | string array | empty | Glob patterns excluded from the code-intel index |

**Per-item sections: happy path first, edge cases in a table.** Each section opens with what the item is for and one real value. Conditions never braid into that prose; they get rows:

| Pattern | Matches |
|---------|---------|
| `vendor/**` | contains a slash, so it matches the full repo-relative path |
| `build/` | trailing slash only, matches nothing — write `build/**` |

**Degradation: what happens when it is wrong.** The closing table answers the support-thread questions before they are asked:

| Condition | Result |
|-----------|--------|
| File absent | Empty config, no error |
| Unknown key | Warning; the rest of the file still loads |
| Malformed TOML | Error, and the caller degrades rather than failing |

## Why this shape

The reader of a reference page has a question, not an afternoon. The opening gives them the model in ten seconds, the inventory routes them to their item, the per-item table keeps conditions out of the sentences they scan, and the degradation table means being wrong has a documented outcome. A reference page that fails usually fails by inverting this: mechanism prose up top, the inventory buried, and every edge case interleaved where the happy path should be.
