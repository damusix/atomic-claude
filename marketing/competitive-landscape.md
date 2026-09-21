# Competitive landscape

The evidence behind the positioning. These numbers are your ammunition. Memorize the SQL price table.


## The empty square: graph and loop, fused

The sharpest empty intersection is not code-intel meets data-lineage. It is **a real code graph meets a disciplined agent loop**. Everyone ships one half:

```
real code graph,         Sourcegraph, OpenGrok, Kythe, ctags/LSP
  no loop ............    you can query structure, but nothing acts on it

agent loop,              Devin, Cursor Composer, Cline, Goose
  no real graph ....     it acts, but on grep + embeddings, so it guesses

atomic claude ......     both, fused: the loop checks the real graph before
                         it acts. Local, free, and the graph is what makes the
                         loop safe and efficient instead of a token bonfire
```

That square is empty. A graph nobody acts on is a search tool; a loop with no graph is a guesser. Atomic is the only one that welds them, locally and for free.

The honest exception is Aider: its repo-map is built with tree-sitter and ranked by personalized PageRank, so it is structural, not grep. But it is ephemeral (rebuilt each session), file-level, token-capped (about 1k tokens by default), and has no SQL lineage. That is a per-session map, not a persistent queryable graph. Name it precisely if it comes up rather than lumping it in with the embedding-based tools, because a reader who uses Aider will catch the overclaim.


## Two markets, one product

Beneath that, Atomic also sits at the intersection of two markets that normally do not overlap:

```
code intelligence ........ Sourcegraph, Cursor, Glean, OpenGrok, ctags/LSP
   (find symbols, callers, blast radius)

data lineage ............. dbt, Collibra, Alation, Atlan, Manta, DataHub
   (track how data flows through SQL)

atomic claude ............ the only free, local tool that does both,
                           and treats SQL as a first-class citizen of
                           the code graph
```

That intersection is empty too. No competitor occupies it.


## Code intelligence: who's there, what's missing

| Tool | Local? | Free? | Graphs SQL? | Position / note |
|------|--------|-------|-------------|-----------------|
| Sourcegraph (+ Cody) | No (cloud upload) | Cody Free/Pro killed July 23 2025; Cody now enterprise-only at ~$59/user/mo | No SQL indexer | The vacuum. Abandoned individuals. |
| Cursor / Windsurf / Augment | No (cloud embeddings) | Freemium, paid for codebase indexing | SQL treated as text | Embeddings cannot answer "what calls this." |
| Greptile | No (cloud) | ~$1/review above 50/mo cap | No | AI code review, not a local graph. |
| Glean (Meta-style / enterprise) | No | Floors ~$60K/yr | No | Enterprise only. |
| OpenGrok / ctags | Yes | Yes | Token-level only | No real call graph, no SQL semantics. |
| Google Kythe | Yes | Yes | No (needs compiler) | No SQL; compiler-bound; not a product. |
| Bloop | Was local | Was free | No | **Archived January 2025.** The one credible local call-graph tool is gone. |

The takeaways:

- **Local-first structural code graph is effectively unoccupied** at production quality. Every commercial leader requires uploading your code.
- **Embeddings (Cursor et al.) cannot do blast radius.** "What breaks if I change this" needs a real call graph, which only Sourcegraph (cloud, paid), Kythe (compiler-bound), and Atomic Claude (local, free) provide.
- **None of them graph SQL.** Not Sourcegraph, not Cursor, not Kythe.
- The category-winning terms for 2026 are **"code graph," "blast radius," and "local-first."** Two 40K-star OSS projects put "CodeGraph" in their names. Use these words.


## Data lineage: the price moat

This is the wedge. Every tool that does what Atomic does for SQL costs enterprise money, requires a warehouse connection, or both. (All figures from cited sources below; treat ranges as directional.)

| Tool | Annual cost | Local / no-DB? | T-SQL deep? | Embedded SQL in app code? |
|------|-------------|----------------|-------------|---------------------------|
| **Atomic Claude** | **$0 (MIT)** | **Yes, fully static** | **Yes** | **Yes (20 host languages)** |
| Collibra | $170K-$197K base; $300K-500K+ with connectors | No | Via connector (paid) | No |
| Alation | $60K entry to ~$413K mid-enterprise | No | Not confirmed | No |
| Atlan | $50K-$120K (25-75 users) | No | Not confirmed | No |
| Monte Carlo | ~$62K median, up to $250K+ | No | Not a focus | No |
| Manta (IBM) | Not public; community est. $150K+ | Yes (static) | Yes | No (means `.sql` files / ETL, not app-code literals) |
| Gudu SQLFlow | Commercial, no public price | Yes (static) | Yes | No |
| dbt Cloud Enterprise | $36K-$84K (20 seats); CLL is Enterprise-only | Needs warehouse | No | No |
| DataHub (OSS) | Free core | No (needs running instance + schemas) | SQLGlot limits apply | No |
| SQLMesh (OSS) | Free core | Yes, but dbt-style models only | No | No |
| sqllineage (OSS) | Free | Yes | T-SQL not a listed dialect | No |
| SQLGlot (OSS lib) | Free | Yes | Documented `ParseError` on prod T-SQL stored procs; `GO` delimiter breaks it | No |


## The four gaps, confirmed

These are the claims you can make that no competitor can refute.

**1. T-SQL lineage is broken across the free tier.**
SQLGlot, the parser backing DataHub, SQLMesh, and dbt Cloud's column lineage, has documented failures on production SQL Server stored procedures (the `GO` batch delimiter forces preprocessing; SSMS-exported scripts throw `ParseError`). sqllineage does not even list T-SQL as supported. The only tools that parse T-SQL deeply are Manta and Gudu SQLFlow, both paid. Atomic's coverage (temp tables, table variables, `OUTPUT INTO`, `PIVOT`/`UNPIVOT`, column-level alias resolution) exists nowhere for free.

**2. Embedded SQL in application code is a genuine white space.**
No tool we could find, commercial or open-source, extracts SQL from string literals inside Go/Python/TypeScript/Java/etc. and graphs it. Frame this as a market gap you verified by searching, not as a cited result. (Adjacent academic work, Xpose / Hidden Query Extraction, arXiv 2504.10898, tackles a *different and harder* problem: recovering a query from an opaque executable via input-output examples. It does not address static extraction from source literals, so do not cite it as proof of this gap.) This is still your single most unique claim. Atomic ships it for 20 host languages. Lead the data-engineering channel with it.

**3. Column-level lineage, free and fully offline, barely exists.**
DataHub needs a running backend with pre-ingested schemas. SQLMesh only does dbt-style model files. Every tool with broad-dialect column lineage at production quality is behind an enterprise paywall. Atomic does it from `.sql` files (and app code) with no DB connection, free.

**4. The price gap is the headline number.**
Collibra and Alation, the platforms closest to the governance depth Atomic's code-intel enables, start at $170K and $60K/year. Implementation services add 30-50% in year one. Atomic is MIT and free. "Enterprise-level power, free" is not marketing puffery here; it is a verifiable price comparison.


## The Karpathy / OKF tailwind

- **Karpathy's LLM Wiki** (GitHub Gist, April 4, 2026): a three-layer knowledge system (immutable sources, LLM-maintained wiki, schema). Spawned dozens of implementations. His own grew to ~100 articles / ~400K words he never wrote. He joined Anthropic on May 19, 2026.
- **Google's Open Knowledge Format (OKF) v0.1** (June 12, 2026): a vendor-neutral markdown+YAML spec for AI-agent knowledge bases, directly inspired by the LLM-wiki pattern. **Always expand the acronym** ("Google's Open Knowledge Format"); bare "OKF" confuses.
- Atomic's `atomic wiki scan/stale/refresh` pipeline is a working implementation of both: sources (repo files), wiki (synthesized knowledge pages), schema (project signals). This is prior art, not bandwagoning. The honest framing: "We built Karpathy's LLM-wiki pattern for codebases, and it already conforms to Google's Open Knowledge Format."


## Sources

Code-intelligence landscape and GTM sources are in the scratchpad research files; the SQL/lineage figures above trace to:

- dbt Cloud pricing: Vendr, b-eye
- Atlan / Collibra / Monte Carlo pricing: Vendr
- Alation pricing: Amazon Marketplace, Dawiso, CostBench
- SQLGlot T-SQL limits: github.com/tobymao/sqlglot/discussions/3095
- sqllineage dialects: sqllineage.readthedocs.io
- DataHub SQL parsing: docs.datahub.com/docs/lineage/sql_parsing
- Xpose / Hidden Query Extraction: arxiv.org/abs/2504.10898 (2025). Note: a *different* problem (query recovery from opaque executables, not source-literal extraction). Do not cite as embedded-SQL-in-source proof. The "no tool does this" claim is a verified market gap, not a cited result.
- Manta static parsing: nexright.com/products/analytics/manta-data-lineage
- Sourcegraph free-tier exit, Bloop archived, Glean floor: code-intelligence research file
- Karpathy LLM Wiki: gist.github.com/karpathy/442a6bf555914893e9891c11519de94f
- Karpathy to Anthropic: techcrunch.com/2026/05/19

Full cited research with every URL lives in the session scratchpad:
`research-1-codegraph.md`, `research-2-sql-lineage.md`, `research-3-gtm-tactics.md`, `research-4-positioning-okf.md`.
