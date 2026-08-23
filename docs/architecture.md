# Architecture

## System Context

`wfm-rag` is one local Go executable with two access modes:

- short-lived CLI commands for indexing, search, and exact retrieval;
- a long-lived read-only MCP process using newline-delimited JSON-RPC over stdin/stdout.

Each invocation is bound to one target repository root. Markdown is authoritative. The generated JSON index is a complete disposable snapshot of active knowledge.

There is no HTTP listener, database, background worker, watcher, embedding service, or multi-root process.

## Data Flow

```text
<repo>/.agents/knowledge/<category>/**/*.md
                    |
                    v
        cards.CanonicalRoot + cards.Load
        discovery, YAML validation, H1/H2 chunking
                    |
                    v
                 index.Build
        documents + flattened searchable chunks
                    |
                    v
                 index.Write
        validated atomic JSON replacement
                    |
          +---------+---------+
          |                   |
          v                   v
      CLI invocation      MCP process
       index.Load       stable fingerprint reload
          |                   |
          +---------+---------+
                    v
          search.Search / search.Get
```

CLI and MCP are adapters. They must not implement independent filtering, ranking, excerpts, or retrieval.

## Package Ownership

### `cmd/wfm-rag`

Owns:

- command dispatch and flag parsing;
- default/custom index path resolution;
- text and JSON formatting;
- stable CLI error classification and exit codes;
- process startup and stdio transport selection.

The package-local `run` function is the test seam. Reusable packages never write stdout or stderr.

### `internal/cards`

Owns:

- canonical root resolution;
- containment under `.agents/knowledge`;
- allowed categories and filesystem filtering;
- YAML frontmatter parsing and metadata validation;
- duplicate IDs across all statuses;
- required H1 title extraction;
- H2 sectioning and bounded chunks;
- source warnings and limits.

`paths` remain opaque citations. This package does not open them.

### `internal/index`

Owns:

- the serialized format and `Version`;
- conversion from cards into documents/chunks;
- structural consistency validation;
- root-bound loading;
- default root-hash path;
- bounded file reads;
- secure atomic replacement.

Any incompatible serialized shape change requires an explicit version decision and an update to `docs/index-format.md`.

### `internal/search`

Owns:

- query, filter, limit, and ID validation;
- Unicode-aware deterministic token normalization;
- filter matching;
- scoring and ordered reasons;
- best-chunk-per-card deduplication;
- deterministic final ordering;
- bounded excerpts;
- exact case-sensitive card retrieval.

This package is transport-neutral and performs no I/O.

### `internal/mcpserver`

Owns:

- MCP server identity and the two typed tools;
- read-only/closed-world annotations;
- semantic validation before index access;
- index fingerprinting and stable reload;
- request cancellation boundaries;
- typed structured output plus SDK text fallback.

Default index-path selection remains in the command package. MCP handlers use shared `search` request/response types.

## Corpus Trust Boundary

Only regular lowercase `.md` cards under these exact directories are candidates:

- `patterns`
- `anti-patterns`
- `dependencies`
- `incidents`
- `sources`

The supplied repository root may be a symlink and is canonicalized. The internal `.agents` and `.agents/knowledge` directories must be real directories. Inner symlinks and non-regular files are ignored rather than followed.

Limits:

| Boundary | Limit |
| --- | ---: |
| One candidate Markdown file | 1 MiB |
| Candidate source corpus | 100 MiB |
| Candidate card count | 10,000 |
| Card ID | 256 UTF-8 bytes |
| Search chunk | 8,000 Unicode characters |
| JSON index | 512 MiB |

Inactive cards are parsed, validated, counted against corpus bounds, and checked for duplicate IDs before they are skipped.

## Card And Chunk Model

The first H1 is the card title. Search chunks are formed from:

1. the H1 section through the first H2;
2. each later H2 section;
3. paragraph-boundary splits when a section exceeds 8,000 Unicode characters;
4. hard splits when one paragraph alone exceeds the limit.

H3 and lower headings stay inside their H2 section. Text before the first H1 remains in exact `get` content but is not searchable.

Chunks carry a deterministic ordinal, source line range, card metadata, source path, and heading. The ordinal is internal: it participates in ties but is omitted from result JSON.

## Deterministic Search

Normalization lowercases text and splits at any rune that is not a Unicode letter or number. Query terms are deduplicated in first-seen order.

There is no stemming, synonym expansion, stop-word removal, fuzzy matching, or semantic similarity.

Each chunk receives:

| Score | Match |
| ---: | --- |
| +100 | Entire normalized query equals the normalized card ID |
| +40 | Exact package match from filter or entire query |
| +30 | Citation path match from filter or entire query |
| +20 | At least one trigger or scope match |
| +10 | Entire query appears as a token-boundary phrase |
| +3 | Each unique query term in title or heading |
| +1 | Each unique query term in chunk text |

The highest-scoring chunk per card is retained. An intra-card tie prefers the lower ordinal. Final order is:

1. score descending;
2. source path ascending;
3. ordinal ascending.

Zero-score chunks are omitted. Score reasons are emitted in formula order so the result is explainable and testable.

## Filters

- Values within one filter kind are ORed.
- Path, package, and type filter kinds are ANDed together.
- Types are exact and case-sensitive.
- Packages are case-insensitive.
- Paths normalize separators and match exact citations or descendants on a path boundary.

Filters are restrictions. Package and path filters also add their score boost to matching candidates.

## Index Persistence

Every build creates the complete index in memory and validates it before persistence. `index.Write`:

1. creates missing directories with mode `0700`;
2. creates a temporary sibling file;
3. sets mode `0600`;
4. writes bounded indented JSON;
5. syncs and closes the file;
6. renames it over the destination.

A malformed corpus or pre-rename write failure leaves the previous valid index untouched.

The loader rejects missing, symlinked, non-regular, oversized, malformed, trailing-data, unsupported-version, root-mismatched, and structurally inconsistent indexes.

## Freshness

CLI search/get load and validate the configured index for every process invocation.

The MCP process keeps one in-memory snapshot. Before each valid search or get operation it compares:

- file size;
- modification time;
- file identity (`os.SameFile`).

When changed, it checks the fingerprint before and after `index.Load`. It publishes the replacement only when the same file was loaded stably and validated for the fixed root.

A failed reload returns `index reload failure`. The server does not silently serve its stale in-memory snapshot. A later valid atomic replacement recovers without restarting.

## MCP Transport

The server uses the official Go SDK pinned in `go.mod` and exposes exactly:

- `agent_knowledge_search`
- `agent_knowledge_get`

Success includes structured content and a JSON text fallback. Tool errors are MCP tool errors, not CLI error envelopes.

Transport invariants:

- newline-delimited JSON-RPC over stdio;
- protocol output only on stdout;
- diagnostics only on stderr;
- cancellation propagated through handler contexts;
- stdin EOF shuts down the process.

## Error Ownership

Core packages define sentinel errors and add operation/path context. The CLI maps known errors to stable JSON error codes and exit status. MCP lets typed SDK handlers convert ordinary operation errors into tool errors.

Invalid MCP input is checked before reload state so a malformed request remains an input error even if the index file is currently broken.

## Non-Goals

Do not add these without a measured requirement and explicit design change:

- HTTP/remote MCP transport;
- databases or background services;
- embeddings or vector search;
- automatic watching/indexing;
- write-capable MCP tools;
- one process serving multiple roots;
- source-file indexing through citations;
- dependency injection or interfaces without multiple real implementations.
