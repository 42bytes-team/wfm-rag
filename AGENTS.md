# AGENTS.md

## Purpose

This file defines default behavior for AI coding agents working on `wfm-rag`. Follow it together with the parent `Services/AGENTS.md` and any matching skill under `.agents/skills/`.

The goal is a small, deterministic local research tool. Prefer the simplest implementation that satisfies the plan; do not design for hypothetical scale or public distribution.

## Skill Routing

| Task | Skill |
| --- | --- |
| Any Go implementation under this repository | `.agents/skills/go-backend/SKILL.md` |
| MCP tools, stdio transport, typed schemas, or protocol tests | `.agents/skills/mcp-development/SKILL.md` |
| Adding, changing, or reviewing tests | `.agents/skills/go-test-writing/SKILL.md` |
| Planning, design, scoping, or comparison | `.agents/skills/implementation-planning/SKILL.md` |
| Code or plan review | `.agents/skills/code-review/SKILL.md` |
| Git status, staging, commits, or repository operations | `.agents/skills/git-workflow/SKILL.md` |

Load every matching skill. MCP implementation normally requires `go-backend`, `mcp-development`, and `go-test-writing`.

## Source Of Truth

`docs/planning/agent-rag-mcp-implementation.md` is the v0.1 implementation contract. If implementation needs to deviate materially, update the plan or ask before broadening scope.

## Toolchain

- The Go module is at the repository root.
- Module path: `wfm-rag`.
- Use Go 1.25 or newer; the local toolchain is Go 1.26.
- Prefer standard library packages unless the plan names a dependency.
- Run `go test -race -count=1 ./...` after Go changes.
- Run `go vet ./...` after Go changes.

## Architecture

- `cmd/wfm-rag` owns argument parsing, output formatting, and process startup.
- `internal/cards` owns corpus discovery, frontmatter parsing, validation, and chunk construction.
- `internal/index` owns the versioned JSON format, atomic writes, loading, and root/version validation.
- `internal/search` owns deterministic normalization, filtering, ranking, reasons, and excerpts.
- `internal/mcpserver` adapts search/get behavior to typed MCP tools and stdio transport.
- CLI and MCP must use the same core search and retrieval behavior.

Do not add interfaces, dependency injection, databases, background workers, HTTP servers, embeddings, or multi-project routing unless a current requirement needs them.

## Required Invariants

- MCP is read-only in v0.1.
- One MCP process serves one root selected at startup; model tool inputs cannot choose another root.
- Only categorized Markdown cards under `.agents/knowledge/` are indexed.
- Symlinks, non-regular files, oversized files, and non-card documentation are not indexed.
- Frontmatter `paths` are citations and ranking hints, not permission to read source files.
- Only `active` cards are searchable.
- Ranking and tie-breaking are deterministic and explainable.
- Reindexing atomically replaces the complete JSON index.
- MCP stdout contains protocol messages only; diagnostics go to stderr.
- A failed reload returns an error rather than silently serving stale in-memory data.

## Work Process

1. Read the relevant plan section and nearby implementation.
2. Trace the CLI and MCP path through the shared core before changing behavior.
3. Make the smallest coherent change.
4. Add focused tests for the observable contract and important failure path.
5. Run formatting, tests, race detection, and vet.
6. Review the diff for accidental scope growth or protocol output.

## Git

Do not commit, amend, rebase, reset, stash, or push unless explicitly requested. Keep changes in this repository separate from the API repository.
