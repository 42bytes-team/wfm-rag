---
name: go-implementation
description: Use first for any Go implementation in wfm-rag, including CLI commands, card parsing, JSON indexing, deterministic search, package ownership, and error contracts.
---

# Go Implementation

Load `go-test-writing` for behavior changes and `mcp-development` for MCP code. Use `agent-knowledge` when card schema/discovery/lifecycle changes.

## Module And Dependencies

- Module root is the repository root; module path is `wfm-rag`.
- Use the Go version in `go.mod`.
- Approved direct dependencies are the official MCP Go SDK, `github.com/google/jsonschema-go`, `gopkg.in/yaml.v3`, and `github.com/yuin/goldmark`.
- No external runtime/test services exist.

## Ownership Map

### `cmd/wfm-rag`

Entry points: `run` dispatching `runIndex`, `runSearch`, `runGet`, `runMCP`, and `runProject`; registry-backed project resolution; `classifyError`.

Own flags, positional validation, project resolution through `internal/project`, `--index` location overrides, text/JSON output, stable CLI error codes, exit status, and process transport. Keep reusable logic out of this package.

### `internal/cards`

Public surface: `CanonicalRoot`, `Load`, `IsValidType`, `IsValidStatus`, `IsCardSource`; `Metadata`, `Card`, `Chunk`, `Warning`, `Corpus`; `MaxFileBytes`, `MaxCorpusBytes`, `MaxCards`, `MaxChunkChars`, `MaxIDBytes`; `ErrMalformedCard`, `ErrDuplicateID`.

Own filesystem containment, discovery, YAML, statuses, duplicate IDs, title extraction, section chunking, line ranges, and corpus warnings. Preserve `WalkDir` lexical order; sort any new map-derived output.

### `internal/index`

Public surface: `Version`, `MaxIndexBytes`; `Index`, `Document`, `BuildResult`; `Build`, `Rebuild`, `StateDir`, `DefaultPath`, `DefaultPathFromCanonical`, `Write`, `Load`; `ErrMissing`, `ErrInvalid`, `ErrUnsupportedVersion`, `ErrRootMismatch`.

Own serialized shape, document/chunk flattening, structural consistency, canonical-root binding, bounded reads, and secure atomic replacement. For any serialized field or validation change, decide explicitly whether old JSON remains compatible. Increment `Version` when it does not and update `docs/index-format.md`.

### `internal/search`

Public surface: `DefaultLimit`, `MaxLimit`, `MaxQueryBytes`, `MaxCardIDBytes`, `MaxFilterValues`, `MaxFilterBytes`, `MaxExcerptChars`; `Query`, `Response`, `Result`, `GetResponse`; `ValidateQuery`, `Search`, `ValidateID`, `Get`; `ErrInvalidArguments`, `ErrCardNotFound`.

Own all retrieval semantics for exactly one index and one named project. It is pure and transport-neutral. Preserve one-best-chunk-per-card dedup, reason order, score/source/ordinal ordering, and exact case-sensitive get.


### `internal/project`

Public surface: `Load`, `Init`, `Remove`, `Statuses`, `Healthcheck`, `PurgeDangling`, `ProjectPaths`, `RegistryPath`; `Registry`, `Entry`, `InitResult`, `Status`, `Finding`; `MaxRegistryBytes`, `MaxProjects`; `ErrRegistryMissing`, `ErrRegistryInvalid`, `ErrIDInvalid`, `ErrUnknownID`, `ErrLimitExceeded`, `ErrLocked`.

Own the registry file, the `flock(LOCK_EX|LOCK_NB)` writer lock, ID syntax, stored-root validation, strict parsing with duplicate-key detection, sorted rendering, statuses without opening indexes, the healthcheck sweep, and dangling-index purge safety. Never write into target repositories.

### `internal/scaffold`

Public surface: `Ensure`, `ReadProfileStatus`, `ProfileStatus`, `KBReadmeFile`, `SkillFile`.

Own the embedded KB skeleton that `project init` creates — the seven category directories, `.agents/knowledge/README.md` carrying the profile-filling prompt, `.agents/knowledge/schema.md`, the eight card templates under `.agents/knowledge/templates/`, and the starter `agent-knowledge` skill with the project ID rendered in — and its create-only filesystem behavior: existing files, directories, and symlinks are never overwritten, displaced, or written through, and a wrong-kind skeleton path is an error rather than a replacement. No other package writes into a target repository.

### `internal/mcpserver`

Public surface: `Source`, `LoadWarning`, `New`, `ErrReload`, `SearchInput`, `GetInput`, the `agent_knowledge_search` and `agent_knowledge_get` tools.

## MCP Adapter

Own typed tool adaptation with a launch-scoped `project` enum, project-keyed in-memory state (one validated entry per launched project), select-then-reload with stable fingerprints scoped to the selected project, degraded startup warnings, and cancellation boundaries. Default index path resolution remains in `cmd`; MCP reuses `search.Response` and `search.GetResponse` and never duplicates retrieval behavior.

## Data Flow

```text
index command: registry -> project root -> cards.Load -> index.Build -> index.Write
CLI retrieval: flags -> registry lookup -> index.Load -> search.Search/Get -> formatter
MCP retrieval: typed validation -> select project -> stable per-project reload -> search.Search/Get -> SDK output
```

Trace the whole affected path, but change behavior in its owner only.

## Implementation Rules

- Prefer concrete structs/functions. Add an interface only for multiple real implementations or a required boundary.
- Search is pure. Cards and index perform bounded filesystem I/O but currently expose synchronous APIs. Do not add context parameters solely for symmetry; if new I/O is genuinely cancellable, assess the owning API deliberately. MCP checks cancellation around current reload/core work.
- Return sentinel/typed errors from owners and wrap with operation/path context using `%w`; adapters format them.
- Bound files, corpora, indexes, queries, filters, IDs, limits, chunks, and excerpts at their input boundary.
- Preserve deterministic discovery, map output, scoring, reason, and JSON ordering.
- Keep stdout/stderr out of reusable packages. MCP stdout belongs exclusively to SDK transport.
- Do not read files cited by card `paths`.
- Do not add HTTP, databases, watchers, embeddings, background workers, or corpus inference. One process serves the projects configured at launch; every retrieval call names exactly one project resolved through the registry (`docs/planning/project-scoped-knowledgebase.md`). Tool inputs cannot select roots, index paths, or write operations.
- Change public behavior and its documentation together. `README.md` plus the focused `docs/` owner update in the same change; AGENTS.md "Documentation Sync" names which section each change touches.

## Common Change Recipes

### Card metadata or chunking

Change cards types/parser, add focused card tests, then update index validation/format if serialized data changes. Update knowledge-authoring docs.

### Index shape

Change index types/validation, make an explicit version decision, test round-trip and malformed structures, preserve atomic failure behavior, update index docs.

### Ranking or filters

Add a concrete failing search case first. Change only `internal/search`. Preserve reasons/ties and run CLI/MCP parity. Record dogfood evidence before broad formula changes.

### CLI

Change command parsing/formatting only. Preserve flag-before-positional behavior, stdout/stderr separation, exit codes, and stable JSON error codes.

### Errors

Define the sentinel in the owner, wrap it, map it in CLI when public, and test `errors.Is`, CLI JSON, and MCP tool behavior as applicable.

## Verification

```bash
gofmt -w cmd/wfm-rag/*.go internal/*/*.go
go test ./internal/search # choose cards, index, search, or mcpserver
go test ./cmd/wfm-rag     # CLI and integration owner
go test -race -count=1 ./...
go vet ./...
```

Run `go mod verify` for dependency changes. Treat an unupdated `README.md` or focused doc as an unfinished change, not as a later documentation pass.
