# Realm bucket reference

Use `atomic-wiki` for realm resolution, bucket registration, capture, indexing, and queries. Realm wiki prose is not synthesized or refreshed automatically.

## Bucket frontmatter

Bucket documents may carry these keys:

| Key | Writer | Meaning |
|---|---|---|
| `title` | author | Falls back to the first H1, then the filename stem. |
| `type` | author | Free-form, such as `Research` or `Ticket`. |
| `description` | author | One line; falls back to the first prose line. |
| `tags` | author | YAML list; a bare string reads as one tag. |
| `status` | author | Free-form, such as `active` or `done`. |
| `created` | scaffold | Stamped by `atomic wiki bucket doc`. |

Every key is optional at index time. Documents without frontmatter appear under `### Unindexed`.

## Managed index regions

`atomic wiki scan` and `atomic wiki bucket index` maintain `<bucket-docs>` in each eligible bucket index. In an unbound realm, they also maintain `<wiki-bucket-list>` in `wiki/index.md`. Do not hand-edit these regions. In a bound realm, use the usable registrations returned by `atomic wiki bucket list` and keep private paths out of shared files.
