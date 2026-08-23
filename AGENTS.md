# AGENTS.md

## Purpose And Scope

This file is the fast project map for AI coding agents working on `wfm-rag`. Load every matching skill under `.agents/skills/` before specialized work.

`wfm-rag` is a small deterministic local knowledge retrieval tool. Markdown cards are indexed into a versioned JSON snapshot and served through a CLI and read-only stdio MCP. Prefer the smallest implementation that preserves containment, determinism, CLI/MCP parity, and protocol safety.

## Parent Instructions

Inherit the parent [`../AGENTS.md`](../AGENTS.md) general safety, review, clarity, Git, and communication rules.

The parent's API-specific instructions do **not** apply here: this repository has no `src/` module root, Makefile, Docker Compose stack, Redis, MongoDB, RabbitMQ, generated protobufs, service-layer packages, or API app skills. The skill-routing table, toolchain, and architecture below replace all parent routing and project-specific sections.

## Skill Routing

| Task | Skill |
| --- | --- |
| Any Go implementation | `.agents/skills/go-backend/SKILL.md` |
| MCP tools, schemas, stdio, reload, protocol compatibility | `.agents/skills/mcp-development/SKILL.md` |
| Adding, changing, repairing, or reviewing tests | `.agents/skills/go-test-writing/SKILL.md` |
| Searching, retrieving, adding, updating, drafting, activating, retiring, deleting, validating, listing, or evaluating knowledge cards | `.agents/skills/knowledge-authoring/SKILL.md` |
| Bug reports, failures, regressions, or root-cause investigation | `.agents/skills/debug-investigation/SKILL.md` |
| Planning, design, scoping, research, comparison | `.agents/skills/implementation-planning/SKILL.md` |
| Code, diff, PR, plan, or documentation review | `.agents/skills/code-review/SKILL.md` |
| Git status, staging, commits, branches, or repository operations | `.agents/skills/git-workflow/SKILL.md` |

Load all that apply. MCP behavior changes normally require `go-backend`, `mcp-development`, and `go-test-writing`. Knowledge-schema or lifecycle changes require `knowledge-authoring` plus the owning Go/test skills.

Agent clients commonly load instructions and skills only at startup. Restart the client after editing `AGENTS.md` or skill files before validating discovery behavior.

## Source Of Truth

- Current source and tests define implemented behavior.
- `README.md` is the human operational contract.
- `docs/architecture.md` is the package/data-flow/invariant map.
- `docs/cli.md`, `docs/knowledge-authoring.md`, `docs/index-format.md`, and `docs/mcp-clients.md` own their specific public contracts.
- `docs/planning/agent-rag-mcp-implementation.md` records v0.1 design intent and history. Read only the affected section; it is not the sole current contract.

If source, tests, and documentation disagree, report and resolve the discrepancy rather than silently broadening behavior.

## Toolchain

- Module root: repository root.
- Module path: `wfm-rag`.
- Use the Go version declared by `go.mod`, currently Go 1.26.5.
- Dependencies are intentionally limited to the official MCP Go SDK, `gopkg.in/yaml.v3`, and `github.com/yuin/goldmark` plus their transitives.
- No external services are needed.

Required after Go changes:

```bash
gofmt -w cmd/wfm-rag/*.go internal/*/*.go
go test -race -count=1 ./...
go vet ./...
```

Also run `go mod verify` after dependency/module changes and `git diff --check` before handoff.

## Project Map

| Path | Owns | Primary tests/docs |
| --- | --- | --- |
| `cmd/wfm-rag/main.go` | command dispatch, flags, path resolution, text/JSON output, error codes, exit status, MCP startup | `cmd/wfm-rag/main_test.go`, `cmd/wfm-rag/integration_test.go`, `docs/cli.md` |
| `internal/cards/cards.go` | canonical roots, corpus discovery, frontmatter, statuses, duplicate IDs, title/chunk construction, corpus limits | `internal/cards/cards_test.go`, `docs/knowledge-authoring.md` |
| `internal/index/index.go` | versioned JSON, flattening, structural validation, default path, bounded loading, atomic writes | `internal/index/index_test.go`, `docs/index-format.md` |
| `internal/search/search.go` | query/filter/ID validation, normalization, filters, ranking, reasons, card dedup, excerpts, exact get | `internal/search/search_test.go`, `docs/architecture.md` |
| `internal/mcpserver/server.go` | two typed read-only tools, fixed-root state, fingerprint reload, cancellation, SDK transport adapter | `internal/mcpserver/server_test.go`, command integration tests, `docs/mcp-clients.md` |
| `internal/testdata/corpus` | realistic shared integration corpus | command integration tests |

## Public Entry Points

- Cards: `CanonicalRoot`, `Load`, `IsValidType`, `IsValidStatus`, `IsCardSource`; `Metadata`, `Card`, `Chunk`, `Warning`, `Corpus`; limits and `ErrMalformedCard`/`ErrDuplicateID`.
- Index: `Build`, `Rebuild`, `DefaultPath`, `Write`, `Load`, `Version`; `Index`, `Document`, `BuildResult`; `MaxIndexBytes` and index sentinels.
- Search: `ValidateQuery`, `Search`, `ValidateID`, `Get`; `Query`, `Response`, `Result`, `GetResponse`; bounds and `ErrInvalidArguments`/`ErrCardNotFound`.
- MCP: `New`, `Run`, `ErrReload`.
- Command package: unexported `run` dispatches `runIndex`, `runSearch`, `runGet`, and `runMCP`; it is the CLI test seam.

## Data Flow

```text
categorized Markdown
  -> cards.CanonicalRoot / cards.Load
  -> YAML validation + Goldmark sectioning
  -> index.Build + structural validation
  -> index.Write atomic JSON replacement

CLI search/get
  -> flags + resolveIndexPath
  -> index.Load
  -> shared search.Search / search.Get
  -> text or JSON output

MCP search/get
  -> SDK schema + semantic validation
  -> stable fingerprint reload
  -> shared search.Search / search.Get
  -> structured output + JSON text fallback
```

Never implement ranking, filters, excerpts, or exact retrieval separately in an adapter.

## Operational Contract

- Commands: `index`, `search`, `get`, `mcp`, and top-level help.
- There are no list/add/update/delete/retire/validate commands; card lifecycle is Markdown editing plus reindex.
- `mcp` is stdio only. There is no HTTP server, URL, port, REST API, SSE, or Streamable HTTP transport.
- The MCP client launches the process and must close stdin for shutdown.
- One process serves one fixed root; configure multiple named processes for multiple repositories.
- Source Markdown changes are invisible until `index` succeeds.

## Corpus Invariants

- Only regular files with an exact lowercase `.md` extension under `patterns`, `anti-patterns`, `dependencies`, `incidents`, or `sources` are candidates; filename stems may use other casing.
- `.agents` and `.agents/knowledge` must be real directories. Inner symlinks/non-regular files are ignored.
- `README.md`, uncategorized docs, skills, `AGENTS.md`, source code, and cited files are not indexed.
- Required metadata: `id`, valid `type`, valid `status`, and first H1 title.
- IDs are unique across active, draft, and retired cards.
- Only active cards enter index documents/chunks; inactive cards are still parsed and validated.
- `paths` are citations/ranking hints, never permission to read source files.
- Limits: 1 MiB/card, 100 MiB corpus, 10,000 cards, 256-byte ID, 8,000-rune chunk, 512 MiB index.

## Index Invariants

- Format is root-bound and currently `Version == 1`.
- Documents/chunks must remain structurally consistent and deterministic.
- Default path is `~/.local/share/wfm-rag/<canonical-root-sha256>/index.json`; `$XDG_DATA_HOME` is not used.
- Write a `0600` temporary sibling, sync, close, and rename. Newly created directories are `0700`.
- A failed build/write preserves the previous valid index.
- Load rejects missing, unsafe, oversized, malformed, trailing-data, unsupported, root-mismatched, or inconsistent indexes.

## Search Invariants

- Query: non-whitespace valid UTF-8, max 4,096 bytes.
- Explicit limit: 1-20; default 5.
- Filters: max 20 values/kind and 1,024 bytes/value; OR within kind, AND across kinds.
- Lexical normalization only: no stemming, synonyms, stop-word removal, fuzzy, or semantic search.
- Keep one best chunk/card. Final tie order: score descending, source ascending, ordinal ascending.
- Zero-score chunks are omitted. Score reasons remain ordered and explainable.
- `Get` is exact and case-sensitive.

## CLI And MCP Invariants

- Invalid CLI input exits 2; operational failures exit 1; success/no-results exits 0.
- Success goes to stdout; errors go to stderr; JSON mode uses stable error codes.
- MCP stdout is protocol-only; diagnostics go to stderr.
- Expose only `agent_knowledge_search` and `agent_knowledge_get`, both read-only and closed-world.
- Tool schemas reject unknown fields and never accept root/project/index/write/shell inputs.
- Validate tool input before reload state.
- Reload fingerprint uses size, modification time, and file identity. Validate the same file before/after load.
- Failed reload returns an error and never serves stale in-memory data.

## Change Routing

- Card discovery/schema/chunking: change `internal/cards`, then assess index compatibility.
- Serialized fields/validation/persistence: change `internal/index`, decide whether to bump `Version`, update index docs.
- Ranking/filter/excerpt: add failed retrieval test first, change only `internal/search`, run CLI/MCP parity.
- CLI flags/output/errors: change `cmd/wfm-rag`, preserve streams/codes, update README/CLI docs.
- MCP tools/reload/protocol: change `internal/mcpserver`, verify pinned SDK signatures, run unit and subprocess tests.
- Public behavior/limits/client compatibility: update the corresponding human docs and this file/skills when agent workflow changes.

## Work Process

1. Identify the owning package from this map.
2. Load matching skills.
3. Read focused tests and only relevant source/docs; do not rescan the whole repository by default.
4. Trace both adapters when shared retrieval behavior changes.
5. Make the smallest coherent change.
6. Add focused success and important failure tests.
7. Update all affected public/agent contracts.
8. Run formatting, race tests, vet, and relevant module/diff checks.
9. Review for scope growth, nondeterminism, unsafe filesystem behavior, and stdout contamination.

## Git

Do not commit, amend, rebase, reset, stash, or push unless explicitly requested. Keep this implementation repository separate from target repositories whose `.agents/knowledge` cards are being indexed. Never stage generated indexes, temporary indexes, or binaries.
