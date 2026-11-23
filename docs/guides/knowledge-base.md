# Organize notes and research in capture buckets


A capture bucket is a folder with an index. Keep research, notes, or exported tickets in buckets so you can find the source without maintaining a separate realm wiki. Each repository can still have its own wiki.

## Set up a bucket


1. Run `atomic where --json` to locate the realm. If it has not been registered, run `atomic wiki scan --root <realm-root>`.
2. Run `atomic wiki bucket add research --root <realm-root>`. Choose a name that describes what goes in the folder. Registering an existing folder can scaffold indexes in its eligible subfolders.
3. Fill in the bucket's `index.md`: say what belongs there and how files are named. Do not edit the managed `<bucket-docs>` section.

## Capture and find an item


From the realm root, create an item:

    atomic wiki bucket doc research vendor-evaluation

This creates `research/vendor-evaluation/index.md` and `research/vendor-evaluation/01_raw/index.md`. Put source material in `01_raw/`. Add a later stage for material with a distinct purpose, such as findings or a draft. Keep the item index useful as a starting point.

Run `atomic wiki bucket index research` to rebuild the listing. Frontmatter supplies titles, descriptions, types, tags, and status; files without it still appear under **Unindexed**. To find an item, read `research/index.md` and follow the link.

## Keep private capture private


In a bound realm, new buckets are private by default. Use `atomic realm status` to see which folders are local, shared, or received but not linked. Sharing a bucket and linking a received bucket are separate actions. Preview either one before applying it. Never put a private bucket's name or purpose in shared steering.

See the [realm capture reference](/reference/realm-wiki) for the commands.
