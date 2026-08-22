---
name: go-backend
description: Use first for any Go implementation in wfm-rag, including CLI commands, card parsing, JSON indexing, deterministic search, and package boundaries.
---

# Go Backend

Use this skill as the project map for all Go work. Load `mcp-development` for MCP code and `go-test-writing` whenever behavior changes.

## Project Shape

The module root is the repository root.

- `cmd/wfm-rag`: thin CLI and process startup.
- `internal/cards`: filesystem discovery, YAML frontmatter, Markdown sections, card validation.
- `internal/index`: versioned JSON index and atomic persistence.
- `internal/search`: pure deterministic retrieval behavior.
- `internal/mcpserver`: read-only MCP adapter over the shared core.
- `internal/testdata`: self-contained corpus fixtures.

Keep logic in the owning package. CLI and MCP adapters must not implement separate ranking or retrieval behavior.

## Implementation Rules

- Prefer concrete structs and functions. Add an interface only when there are multiple real implementations or a boundary requires one.
- Return errors with operation and path context; callers decide how to format them.
- Thread `context.Context` through MCP and potentially cancellable operations.
- Bound file sizes, corpus sizes, query size, result limits, and excerpts at input boundaries.
- Sort filesystem discoveries and map-derived output before assigning IDs, scoring, or encoding results.
- Preserve deterministic tie-breaking with focused tests.
- Write the index to a temporary sibling file, sync it, close it, and rename it over the destination.
- Keep stdout out of reusable packages. CLI output belongs in `cmd`; MCP stdout belongs to the SDK transport.

## Dependencies

Use only dependencies named in the plan unless explicit approval is given:

- official MCP Go SDK
- `gopkg.in/yaml.v3`
- `github.com/yuin/goldmark`

## Verification

```bash
gofmt -w <changed-go-files>
go test -race -count=1 ./...
go vet ./...
```
