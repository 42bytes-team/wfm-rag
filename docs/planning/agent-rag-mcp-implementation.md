# WFM RAG CLI And MCP Implementation Plan

Status: Ready for implementation
Date: 2026-08-04

## Goal

Create a small local research project at `/home/kyckyc/Projects/Warframe/Services/Rag` that indexes curated Markdown knowledge cards and exposes the same read-only retrieval through:

- a `wfm-rag` CLI
- a stdio MCP server

Markdown under a repository's `.agents/knowledge/` directory remains the source of truth. The generated index is disposable derived state.

## Decisions

- Repository directory: `/home/kyckyc/Projects/Warframe/Services/Rag`
- Go module: `wfm-rag`
- Binary: `wfm-rag`
- First release includes both CLI and stdio MCP.
- The MCP server is read-only and exposes `agent_knowledge_search` and `agent_knowledge_get`.
- Knowledge is added or changed by editing Markdown files, then running `wfm-rag index`.
- One MCP process serves one fixed repository root supplied at startup.
- MCP tool inputs cannot select an arbitrary project root.
- The index is a versioned JSON file loaded into memory.
- Retrieval is deterministic lexical matching. No SQLite, embeddings, vector database, watcher, or reranker in v0.1.
- Only `active` cards are searchable. `draft` and `retired` cards are skipped with indexing warnings.

## Non-Goals

- Enforcing that an agent calls MCP. Harness-specific enforcement is separate.
- Writing knowledge through MCP or CLI.
- Multi-project search within one server process.
- Indexing entire repositories, cited source files, skills, or `AGENTS.md` in v0.1.
- Automatically watching Markdown files.
- Supporting raw FTS query syntax or embedding models.

## Current State

The API (`/home/kyckyc/Projects/Warframe/Services/Api`) repository provides the first real corpus:

- `.agents/knowledge/schema.md`
- `.agents/knowledge/patterns/modern-go.md`
- `.agents/knowledge/patterns/reuse-existing-capabilities.md`
- `.agents/knowledge/dependencies/ini-list-parsing.md`
- `.agents/evals/agent-quality/`


The official MCP Go SDK provides typed tools and stdio transport. Pin `github.com/modelcontextprotocol/go-sdk` to a tagged release rather than `main`; `v1.7.0` is the current tagged release reviewed for this plan. Its revised MCP protocol support must be the basis of the stdio integration tests rather than assuming the legacy initialization flow.

## User Workflow

Index a repository:

```bash
wfm-rag index --root /path/to/repository
```

Search from the CLI:

```bash
wfm-rag search --root /path/to/repository "config parser comma separated"
```

Retrieve a complete card:

```bash
wfm-rag get --root /path/to/repository ini-list-parsing
```

Start one MCP server for one repository:

```bash
wfm-rag mcp --root /path/to/repository
```

After adding or editing Markdown knowledge:

```bash
wfm-rag index --root /path/to/repository
```

The running MCP server checks the index file modification time before each request and reloads it when the generated index changes.

## Repository Layout

Keep the initial project compact:

```text
Rag/
  .gitignore
  AGENTS.md
  README.md
  go.mod
  go.sum
  cmd/wfm-rag/main.go
  internal/cards/
    cards.go
    cards_test.go
  internal/index/
    index.go
    index_test.go
  internal/search/
    search.go
    search_test.go
  internal/mcpserver/
    server.go
    server_test.go
  internal/testdata/
    corpus/
  docs/
    index-format.md
    mcp-tools.md
```

Responsibilities:

- `cards`: discover and parse knowledge cards, validate metadata, and split cards into heading sections.
- `index`: define the JSON format, build indexes, write atomically, load indexes, and validate versions.
- `search`: normalize queries, apply metadata hints, rank chunks deterministically, and build excerpts.
- `mcpserver`: expose the search and get operations through typed MCP tools over stdio.
- `cmd/wfm-rag`: parse subcommands and flags, format CLI output, and start MCP mode.

Do not add interfaces until tests need an alternate implementation.

## Dependencies

Use the standard library where practical and keep external dependencies limited to:

- `github.com/modelcontextprotocol/go-sdk/mcp` for MCP
- `gopkg.in/yaml.v3` for frontmatter
- `github.com/yuin/goldmark` for Markdown heading parsing

Use Go 1.25 or newer because the selected MCP SDK requires it.

## Corpus Contract

The default card locations are:

```text
.agents/knowledge/patterns/**/*.md
.agents/knowledge/anti-patterns/**/*.md
.agents/knowledge/dependencies/**/*.md
.agents/knowledge/incidents/**/*.md
.agents/knowledge/sources/**/*.md
```

Rules:

- Resolve the startup root to an absolute path and resolve symlinks in that root before locating `.agents/knowledge`.
- Walk only `.agents/knowledge` under that root.
- Do not follow symlinks.
- Read only regular `.md` files from those category directories.
- Skip category `README.md` files. Do not index the corpus README, index, schema, templates, or RAG notes as cards.
- Reject files larger than 1 MiB.
- Cap one project at 10,000 cards and 100 MiB of source Markdown.
- Treat frontmatter `paths` as citations and ranking hints, not files to index.
- Do not read `.env`, secrets, source code, or arbitrary paths.

Cards follow the existing schema:

```yaml
---
id: ini-list-parsing
type: dependency
status: active
scope:
  - go
triggers:
  - parser
packages:
  - gopkg.in/ini.v1
paths:
  - src/pkgs/config/ini.go
source_confidence: high
last_verified: 2026-07-14
---
```

Validation behavior:

- `id`, `type`, and `status` are required.
- `type` must be one of `pattern`, `anti-pattern`, `dependency`, `incident`, `reference`, or `decision`.
- `status` must be one of `draft`, `active`, or `retired`.
- The first level-one Markdown heading is the required card title.
- Card IDs must be unique within the repository across every status.
- Invalid YAML, missing required fields, and duplicate IDs fail indexing.
- Omitted optional metadata emits a warning; explicitly empty list fields are allowed without a warning.
- `draft` and `retired` cards emit a skipped-card warning and are not added to the searchable index.
- Symlinks and non-regular entries are ignored without being opened or traversed.

## Chunking

Use one chunk per major Markdown section:

- The title and introductory text form the first chunk.
- Each level-two heading starts another chunk.
- Lower-level headings remain within their parent section for v0.1.
- If a chunk exceeds 8,000 characters, split it on paragraph boundaries.
- If one paragraph exceeds 8,000 characters, hard-split that paragraph so the chunk bound is preserved.

Each chunk stores:

- stable card ID
- card title, type, scopes, triggers, packages, and cited paths
- source path relative to the repository root
- heading
- ordinal
- text
- source line range when available

## JSON Index

Default path:

```text
~/.local/share/wfm-rag/<root-hash>/index.json
```

Logical format:

```json
{
  "version": 1,
  "root": "/canonical/repository/root",
  "generated_at": "2026-07-16T12:00:00Z",
  "documents": [],
  "chunks": []
}
```

Index requirements:

- Hash the canonical root with SHA-256 and use the full lowercase hexadecimal digest as the index directory name.
- Store the canonical root inside the index and verify it on load.
- Build the complete index in memory.
- Write to a temporary file in the destination directory, `fsync`, then rename atomically.
- Create index directories with `0700` and files with `0600`.
- Reject generated or loaded indexes larger than 512 MiB.
- Reject unknown index versions.
- Rebuild the complete index on every `index` command; incremental indexing is unnecessary for the initial corpus size.

## Search And Ranking

Normalize query and metadata terms by lowercasing, splitting punctuation, dropping empty tokens, and deduplicating terms.

Use a small deterministic score:

- `+100`: exact card ID match
- `+40`: exact package match
- `+30`: exact cited path or path-prefix hint match
- `+20`: exact trigger or scope match
- `+10`: complete normalized query appears in title, heading, or text
- `+3`: each unique query term present in title or heading
- `+1`: each unique query term present in chunk text

Ranking rules:

- Score each chunk independently.
- Keep the highest-scoring chunk per card in normal search output.
- When chunks from one card tie, keep the lower ordinal.
- Sort by score descending, then source path ascending, then ordinal ascending.
- Exclude zero-score results.
- Treat provided path, package, and type filters as OR within one filter kind and AND across filter kinds.
- A path filter matches an exact cited path or a cited directory prefix on a path-segment boundary.
- Default to 5 results and reject explicit limits outside 1-20.
- Limit excerpts to 800 characters around the first useful match.
- Return score reasons so ranking can be debugged during research.
- Emit score reasons in score-formula order, with multiple matched metadata values sorted ascending.

This formula is deliberately simple. Change it only in response to retrieval tests.

## CLI Contract

### `wfm-rag index`

```bash
wfm-rag index --root <repo> [--index <file>] [--format text|json]
```

Builds and atomically replaces the complete index. Reports indexed cards, skipped cards, chunks, warnings, index path, and generation time.

### `wfm-rag search`

```bash
wfm-rag search --root <repo> [--index <file>] [--path <path>] [--package <pkg>] [--type <type>] [--limit 5] [--format text|json] <query>
```

Loads the existing index and returns ranked card excerpts. It must fail clearly when the index is missing or belongs to another root.

### `wfm-rag get`

```bash
wfm-rag get --root <repo> [--index <file>] [--format text|json] <card-id>
```

Returns one complete indexed card with metadata and source citation. Duplicate IDs are prevented during indexing.

### `wfm-rag mcp`

```bash
wfm-rag mcp --root <repo> [--index <file>]
```

Starts a read-only stdio server. Stdout is reserved for MCP protocol messages; diagnostics go to stderr.

## MCP Tools

MCP inputs do not contain `root` or `project`. The server root is fixed at process startup.

Search queries must contain non-whitespace text and are limited to 4,096 UTF-8 bytes. Card IDs are limited to 256 UTF-8 bytes. Each metadata filter list is limited to 20 non-empty values, and each value is limited to 1,024 UTF-8 bytes.

### `agent_knowledge_search`

Input:

```json
{
  "query": "config parser comma separated",
  "paths": ["src/apps/percy-worker/config.go"],
  "packages": ["gopkg.in/ini.v1"],
  "types": ["pattern", "dependency"],
  "limit": 5
}
```

Output:

```json
{
  "root": "/canonical/repository/root",
  "indexed_at": "2026-07-16T12:00:00Z",
  "query": "config parser comma separated",
  "results": [
    {
      "id": "ini-list-parsing",
      "type": "dependency",
      "title": "INI List Parsing",
      "score": 64,
      "reasons": ["package: gopkg.in/ini.v1", "trigger: config", "terms: config parser comma separated"],
      "excerpt": "Read the exact section and key, then use Key.Strings...",
      "source": ".agents/knowledge/dependencies/ini-list-parsing.md",
      "heading": "Guidance",
      "citations": ["src/pkgs/config/ini.go"]
    }
  ]
}
```

### `agent_knowledge_get`

Input:

```json
{
  "id": "ini-list-parsing"
}
```

Output includes card metadata, complete Markdown content, source path, and citations.

Use typed input and output structs with generic `mcp.AddTool`. Return structured output and the SDK-compatible serialized JSON text fallback.

## Freshness

Freshness is intentionally explicit:

- Editing Markdown does not update the index automatically.
- The user runs `wfm-rag index --root <repo>` after changes.
- CLI search/get load the index for each invocation.
- MCP records the index modification time and reloads before a valid search or get operation when the file changes.
- The reload fingerprint also includes file size and file identity so an atomic replacement is detected even if its modification time is unchanged.
- Responses include `indexed_at` so stale indexes are visible.
- If reload fails, return an error instead of silently using an older in-memory index.

## Error Contract

CLI commands use non-zero exit status and write errors to stderr.

JSON mode and MCP errors should distinguish:

- invalid arguments
- invalid or unsupported index
- index missing
- index belongs to another root
- malformed card corpus
- duplicate card ID
- card not found
- index reload failure

Do not expose stack traces or absolute paths other than the configured local root and index path.

## Testing

Unit tests:

- valid, partial, malformed, draft, and retired cards
- duplicate IDs
- heading chunking and oversized-section splitting
- symlink and non-regular file rejection
- file and corpus size limits
- deterministic ranking, filters, tie-breaking, limits, and excerpts
- index version and root validation
- atomic index replacement
- missing and malformed index errors
- MCP input validation and structured output

Integration tests:

- build an index from `internal/testdata/corpus`
- config-parser query returns `ini-list-parsing` first
- pointer-helper query returns `modern-go` first
- running MCP server reloads after an atomic reindex
- stdio subprocess completes the SDK's current initialization handshake, tools/list, search, and get
- every MCP stdout line is valid protocol output and diagnostics stay on stderr
- server exits cleanly on stdin EOF and cancellation

Manual dogfood:

```bash
go test ./...
go run ./cmd/wfm-rag index --root ../Api
go run ./cmd/wfm-rag search --root ../Api "config parser comma separated"
go run ./cmd/wfm-rag get --root ../Api ini-list-parsing
go run ./cmd/wfm-rag mcp --root ../Api
```

## Implementation Steps

1. Completed: create `../Rag`, initialize Git, and run `go mod init wfm-rag`.
2. Completed: add README, `.gitignore`, and a small repository-specific `AGENTS.md`.
3. Completed: add the three pinned dependencies and confirm the selected Go version builds them.
4. Completed: implement card discovery, frontmatter parsing, validation, heading chunks, and focused tests.
5. Completed: implement the versioned JSON index, size-bounded loading, and atomic full rebuild with preservation tests.
6. Completed: implement deterministic search, strict filters and limits, score reasons, Unicode-safe bounded excerpts, per-card deduplication, and exact card retrieval.
7. Completed: implement and dogfood the `index`, `search`, and `get` CLI commands with text/JSON output and stable error codes.
8. Completed: implement the read-only stdio MCP server and two typed tools.
9. Completed: add unit, integration, and stdio protocol tests.
10. Completed: dogfood against `../Api` and record retrieval results and failures in [`api-dogfood-2026-08-22.md`](api-dogfood-2026-08-22.md) before changing ranking.
11. Completed: document OpenCode, OMP, and generic MCP client configuration in [`docs/mcp-clients.md`](../mcp-clients.md).

## Acceptance Criteria

- `go test ./...` passes in `../Rag`.
- The binary indexes the API knowledge corpus without reading outside `.agents/knowledge`.
- The Percy config query returns the `ini-list-parsing` card with its INI guidance and citation.
- CLI and MCP produce equivalent search ordering and card content.
- MCP tool inputs cannot select another root or write files.
- Reindexing atomically updates a running MCP server on its next valid search or get operation.
- MCP stdout remains protocol-only.

## Deferred Work

Only consider these after the v0.1 retrieval tests justify them:

- multi-project server configuration
- SQLite or another persistent search engine
- embeddings or hybrid retrieval
- indexing cited source contents
- automatic filesystem watching
- agent-authored knowledge tools
- HTTP MCP transport
- harness-specific retrieval enforcement

## Risks And Rollback

- Poor lexical recall: add retrieval cases and adjust the explicit score formula; do not add embeddings without evidence.
- Stale index: expose `indexed_at`, document manual reindexing, and fail requests when reloads fail.
- Malformed knowledge: fail indexing without replacing the last valid index.
- Protocol corruption: keep stdout protocol-only and cover it with subprocess tests.
- Target repository mistakes: `../Rag` is separate from the API repository and can be deleted or reinitialized without changing API application code.

## Open Questions

None for the internal research prototype. It is not intended for publication and does not need a license file.

## Execution Skills

- Go implementation and testing guidance appropriate to the new repository.
- MCP SDK documentation for the pinned tagged release.
- Architecture review focused on keeping the local prototype small.
