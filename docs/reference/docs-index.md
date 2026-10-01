# atomic docs index

`atomic docs index` rebuilds the `<bucket-docs>` region of `<dir>/index.md` from the frontmatter of the markdown files beside it, for any directory you name. It uses the renderer behind `atomic wiki bucket index`, without the requirement that the directory be a registered wiki bucket. Everything outside the region stays byte-identical. `--check` renders without writing, so CI can fail a pull request whose index lags its files.


## Usage

    atomic docs index [--check] <dir>...

Each directory is processed and reported, even when an earlier one fails. Relative paths resolve from the current directory. A directory whose name starts with `-` needs `--` before it: `atomic docs index -- -drafts`. An `index.md` without a `<bucket-docs>` region gets one appended; a missing `index.md` is created holding only the region.

Write mode prints one count line per directory:

    $ atomic docs index docs/help/admin
    docs/help/admin: 2 indexed, 0 unindexed

    $ cat docs/help/admin/index.md
    # Admin help

    How the admin portal works.

    <bucket-docs>

    ## Docs

    - [Jobs](jobs.md) - Read the worker queue.
    - [Sources](sources.md) - Register a source and start its crawl. · tags: ingest

    </bucket-docs>

`--check` prints a `STALE` line for each directory whose region differs from a fresh render, prints nothing for a fresh one, and writes nothing. Errors go to stderr:

    $ atomic docs index --check docs/help/admin docs/help/nope
    STALE docs/help/admin/index.md
    atomic docs index: read dir docs/help/nope: open docs/help/nope: no such file or directory
    $ echo $?
    2


## Exit codes

| Code | Write mode | `--check` |
|------|------------|-----------|
| 0 | every directory indexed | every region fresh |
| 1 | a directory failed (missing, unreadable, unpaired `<bucket-docs>` tag) | a region is stale |
| 2 | usage error (no directory, unknown flag, a flag or `--` after a directory) | a directory failed, or a usage error |

Across several directories the highest code wins, so under `--check` an error outranks a stale region. `--check` follows the 0 fresh / 1 stale / 2 error convention of `atomic docs stale`.


## Frontmatter it reads

| Key | Used for |
|-----|----------|
| `title` | Link text. Falls back to the first H1 outside a code fence, then the filename stem. |
| `description` | Text after the link. Past 120 characters it is cut at the last whole word and ends in `…`. Falls back to the first prose line of 15 or more letters; with none, the entry is link-only. |
| `tags` | ` · tags: a, b` suffix. A YAML string list or a single string; any other shape is ignored. |

A file whose frontmatter carries none of `title`, `type`, `description`, `tags`, `status`, or `created` is listed under `### Unindexed` and counted as unindexed. `index.md` itself is never listed. A subdirectory with a sibling `<slug>.md` collapses into that entry as a router; a subdirectory without one is listed as an orphan subtree and counted as unindexed. The listing rules are the same ones [`atomic wiki bucket index`](/reference/realm-wiki) applies to capture buckets.
