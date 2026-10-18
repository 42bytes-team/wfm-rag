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
| Any Go implementation | `.agents/skills/go-implementation/SKILL.md` |
| MCP tools, schemas, stdio, reload, protocol compatibility | `.agents/skills/mcp-development/SKILL.md` |
| Adding, changing, repairing, or reviewing tests | `.agents/skills/go-test-writing/SKILL.md` |
| Searching, retrieving, adding, updating, drafting, activating, retiring, deleting, validating, listing, or evaluating knowledge cards | `.agents/skills/agent-knowledge/SKILL.md` |
| Bug reports, failures, regressions, or root-cause investigation | `.agents/skills/debug-investigation/SKILL.md` |
| Planning, design, scoping, research, comparison | `.agents/skills/implementation-planning/SKILL.md` |
| Code, diff, PR, plan, or documentation review | `.agents/skills/code-review/SKILL.md` |
| Git status, staging, commits, branches, or repository operations | `.agents/skills/git-workflow/SKILL.md` |

Load all that apply. MCP behavior changes normally require `go-implementation`, `mcp-development`, and `go-test-writing`. Knowledge-schema or lifecycle changes require `agent-knowledge` plus the owning Go/test skills.

Agent clients commonly load instructions and skills only at startup. Restart the client after editing `AGENTS.md` or skill files before validating discovery behavior.

## Source Of Truth

- Current source and tests define implemented behavior.
- `README.md` is the human operational contract.
- `docs/architecture.md` is the package/data-flow/invariant map.
- `docs/cli.md`, `docs/knowledge-authoring.md`, `docs/index-format.md`, `docs/mcp-clients.md`, and `docs/troubleshooting.md` own their specific public contracts.
- `docs/planning/agent-rag-mcp-implementation.md` records v0.1 design intent and history; `docs/planning/agent-context-cost-multi-root-mcp.md` records the superseded multi-root merge experiment and its measurements; `docs/planning/project-scoped-knowledgebase.md` records the project-selection design. Read only the affected section; none is the sole current contract.

If source, tests, and documentation disagree, report and resolve the discrepancy rather than silently broadening behavior.

## Toolchain

- Module root: repository root.
- Module path: `wfm-rag`.
- Use the Go version declared by `go.mod`, currently Go 1.26.5.
- Dependencies are intentionally limited to the official MCP Go SDK, `github.com/google/jsonschema-go`, `gopkg.in/yaml.v3`, and `github.com/yuin/goldmark` plus their transitives.
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
| `cmd/wfm-rag/main.go` | command dispatch, flags, project resolution, text/JSON output, error codes, exit status, MCP startup | `cmd/wfm-rag/main_test.go`, `cmd/wfm-rag/integration_test.go`, `docs/cli.md` |
| `internal/cards/cards.go` | canonical roots, corpus discovery, frontmatter, statuses, duplicate IDs, title/chunk construction, corpus limits | `internal/cards/cards_test.go`, `docs/knowledge-authoring.md` |
| `internal/index/index.go` | versioned JSON, flattening, structural validation, state directory and default path, bounded loading, atomic writes | `internal/index/index_test.go`, `docs/index-format.md` |
| `internal/search/search.go` | query/filter/ID validation, normalization, filters, ranking, reasons, card dedup, excerpts, exact get | `internal/search/search_test.go`, `docs/architecture.md` |
| `internal/project/project.go` | project registry read/write/validate, flock writer lock, ID rules, statuses, healthcheck sweep, dangling-index purge, error sentinels | `internal/project/project_test.go`, `docs/cli.md` |
| `internal/scaffold/scaffold.go` | embedded KB skeleton (category dirs, KB `README.md` carrying the profile prompt, `schema.md`, card templates, starter agent-knowledge skill) created create-only by `project init` | `internal/scaffold/scaffold_test.go`, `docs/knowledge-authoring.md` |
| `internal/mcpserver/server.go` | two typed read-only tools, project-keyed state (one entry per launched project), select-then-reload, cancellation, SDK transport adapter | `internal/mcpserver/server_test.go`, command integration tests, `docs/mcp-clients.md` |
| `internal/testdata/corpus` | realistic shared integration corpus | command integration tests |

## Public Entry Points

- Cards: `CanonicalRoot`, `Load`, `IsValidType`, `IsValidStatus`, `IsCardSource`; `Metadata`, `Card`, `Chunk`, `Warning`, `Corpus`; limits and `ErrMalformedCard`/`ErrDuplicateID`.
- Index: `Build`, `Rebuild`, `StateDir`, `DefaultPath`, `DefaultPathFromCanonical`, `Write`, `Load`, `Version`; `Index`, `Document`, `BuildResult`; `MaxIndexBytes` and index sentinels.
- Search: `ValidateQuery`, `Search`, `ValidateID`, `Get`; `Query`, `Response`, `Result`, `GetResponse`; bounds and `ErrInvalidArguments`/`ErrCardNotFound`. Retrieval always operates on exactly one index and one named project.
- Project: `Load`, `Init`, `Remove`, `Statuses`, `Healthcheck`, `PurgeDangling`, `ProjectPaths`, `RegistryPath`; `Registry`, `Entry`, `InitResult`, `Status`, `Finding`; `MaxRegistryBytes`, `MaxProjects` and registry sentinels.
- Scaffold: `Ensure(root, projectID)` returns the created repository-relative paths; `ReadProfileStatus(root)` returns `ProfileStatus{Incomplete, PromptFile}` for the starter profile; `KBReadmeFile` and `SkillFile` name the scaffolded paths.
- MCP: `Source`, `LoadWarning`, `New`, `ErrReload`, `SearchInput`, `GetInput`.
- Command package: unexported `run` dispatches `runIndex`, `runSearch`, `runGet`, `runMCP`, and `runProject`; it is the CLI test seam.

## Data Flow

```text
categorized Markdown
  -> cards.CanonicalRoot / cards.Load
  -> YAML validation + Goldmark sectioning
  -> index.Build + structural validation
  -> index.Write atomic JSON replacement

CLI search/get
  -> flags + registry lookup (one project)
  -> index.Load (root-bound)
  -> shared search.Search / search.Get
  -> text or JSON output

MCP search/get
  -> SDK schema (launch-scoped project enum) + semantic validation
  -> select project, then stable fingerprint reload of that project only
  -> shared search.Search / search.Get
  -> structured output + JSON text fallback
```

Never implement ranking, filters, excerpts, or exact retrieval separately in an adapter.

## Operational Contract

- Commands: `index`, `search`, `get`, `mcp`, `project init`, `project remove`, `project list`, `project healthcheck`, and top-level help. `project init` also scaffolds the missing KB skeleton in the target repository (see Corpus Invariants).
- There are no card list/add/update/delete/retire/validate commands; card lifecycle is Markdown editing plus reindex. `project list` lists registered projects, not cards.
- `mcp` is stdio only. There is no HTTP server, URL, port, REST API, SSE, or Streamable HTTP transport.
- The MCP client launches the process and must close stdin for shutdown.
- One MCP process serves the projects configured at launch with repeatable `--project` or `--all-projects`, up to 64. The launch arguments are the allowlist; registry membership alone grants nothing.
- Every retrieval call, CLI or MCP, names exactly one project. There is no inference from the working directory and no cross-corpus merging. Card IDs are unique within one corpus; nothing resolves an ID across corpora.
- Registry changes affect new CLI invocations immediately and a running MCP server only after a client restart. Index content reloads live without a restart.
- Source Markdown changes are invisible until `index` succeeds.

## Corpus Invariants

- Only regular files with an exact lowercase `.md` extension under `patterns`, `anti-patterns`, `dependencies`, `incidents`, `research`, `sources`, or `state` are candidates; filename stems may use other casing.
- `.agents` and `.agents/knowledge` must be real directories. Inner symlinks/non-regular files are ignored.
- `README.md`, uncategorized docs, skills, `AGENTS.md`, source code, and cited files are not indexed.
- Required metadata: `id`, valid `type`, valid `status`, and first H1 title.
- IDs are unique across active, draft, and retired cards.
- Only active cards enter index documents/chunks; inactive cards are still parsed and validated.
- `paths` are citations/ranking hints, never permission to read source files.
- Limits: 1 MiB/card, 100 MiB corpus, 10,000 cards, 256-byte ID, 8,000-rune chunk, 512 MiB index.
- `project init` is the only command that writes into a target repository, and only as a create-only skeleton: category directories, `.agents/knowledge/README.md`, `.agents/knowledge/schema.md`, `.agents/knowledge/templates/*.md`, and `.agents/skills/agent-knowledge/SKILL.md`. Every scaffold operation is anchored to an opened root descriptor of the target repository, so no write can leave it. The scaffolded knowledge `README.md` is documentation, never a card candidate, and carries the prompt that fills the starter skill's `Repository Profile`. Existing files, directories, and symlinks are never overwritten, displaced, or written through; a wrong-kind skeleton path is a `project_scaffold_failed` error and the registration itself still stands. No identity or retrieval state is ever stored in a target repository.

## Index Invariants

- Format is root-bound and currently `Version == 1`.
- Documents/chunks must remain structurally consistent and deterministic.
- State directory is `$XDG_DATA_HOME/wfm-rag` when `$XDG_DATA_HOME` is set and absolute, otherwise `$HOME/.local/share/wfm-rag`; a relative or empty `XDG_DATA_HOME` falls back. Default index path is `<state-dir>/<canonical-root-sha256>/index.json`.
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
- Tool schemas reject unknown fields. `project` is required and constrained to a launch-scoped sorted enum; root, index path, write, and shell inputs are never accepted.
- Validate tool input before touching reload state.
- Reload fingerprint uses size, modification time, and file identity. Validate the same file before/after load.
- A failed reload never serves stale in-memory data. Reload is per selected project: a broken or missing index fails only calls that select that project, the server still starts degraded with a stderr warning, and the project recovers on a later call once a valid index exists.
- Registry: `0600` regular file in a `0700` state directory, symlinks and non-regular files refused, strict parsing (trailing data, duplicate keys, unknown fields, non-absolute stored roots rejected), `flock(LOCK_EX|LOCK_NB)` writer serialization with `project_locked` on contention, 1 MiB and 64-project caps, sorted rendering.
- `--index` overrides the index file location only; the expected root stays the selected project's registered root.

## Change Routing

- Card discovery/schema/chunking: change `internal/cards`, then assess index compatibility.
- Serialized fields/validation/persistence: change `internal/index`, decide whether to bump `Version`, update index docs.
- Ranking/filter/excerpt: add failed retrieval test first, change only `internal/search`, run CLI/MCP parity.
- CLI flags/output/errors: change `cmd/wfm-rag`, preserve streams/codes, update README/CLI docs.
- MCP tools/reload/protocol: change `internal/mcpserver`, verify pinned SDK signatures, run unit and subprocess tests.
- Project registry/statuses/healthcheck: change `internal/project`, preserve the flock lock and atomic-write contract, and update CLI docs.
- KB skeleton/README prompt/templates/starter skill: change `internal/scaffold` assets, keep create-only semantics, and update `docs/knowledge-authoring.md`.
- Public behavior/limits/client compatibility: update the corresponding human docs and this file/skills when agent workflow changes.

## Documentation Sync

`README.md` is the human operational contract. A change that alters anything README states must update README in the same change, not in a later documentation pass. Update the focused `docs/` owner too: README carries the tour and the short contract, and the focused doc carries the complete one.

| Change touches | Update here |
| --- | --- |
| commands, flags, defaults, output fields, exit codes, error codes | `README.md` (CLI Reference) and `docs/cli.md` |
| install steps, Go version, dependencies, state directory, file modes | `README.md` (Install, State Directory) and `docs/index-format.md` |
| onboarding flow, `project init` behavior, scaffolding, registry or healthcheck semantics | `README.md` (Onboarding) and `docs/cli.md` |
| MCP client setup, tool names, input schemas, launch allowlist, degraded startup | `README.md` (Connect An LLM Through MCP) and `docs/mcp-clients.md` |
| card categories, types, statuses, frontmatter fields, lifecycle steps, limits | `README.md` (Knowledgebase) and `docs/knowledge-authoring.md` |
| package ownership, data flow, internal invariants | `docs/architecture.md`; README keeps only the summary diagram |
| symptoms and repair commands | `docs/troubleshooting.md`; README carries no troubleshooting list |

Never restate one full contract in two places. When README and a focused doc disagree, fix both against the source and tests rather than copying one into the other.

## Work Process

1. Identify the owning package from this map.
2. Load matching skills.
3. Read focused tests and only relevant source/docs; do not rescan the whole repository by default.
4. Change the owner, not an adapter.
5. Preserve public contracts unless an explicit plan changes them.
6. Add focused success and important failure tests.
7. Update every affected contract in the same change: `README.md` whenever the change touches what README describes, then the focused `docs/` owner, then this file and the skills when agent workflow changes.
8. Run formatting, race tests, vet, and relevant module/diff checks.
9. Review for scope growth, nondeterminism, unsafe filesystem behavior, and stdout contamination.

## Git

Do not commit, amend, rebase, reset, stash, or push unless explicitly requested. Keep this implementation repository separate from target repositories whose `.agents/knowledge` cards are being indexed. Never stage generated indexes, temporary indexes, or binaries.
