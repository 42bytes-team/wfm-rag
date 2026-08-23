---
name: go-test-writing
description: Use when adding, changing, repairing, or reviewing Go tests, fixtures, ranking assertions, filesystem safety, CLI behavior, or MCP protocol tests in wfm-rag.
---

# Go Test Writing

Tests are required for changed behavior. Prefer standard library `testing`, table-driven cases, complete deterministic assertions, and real package boundaries. Do not add a test framework or production injection solely for test convenience.

## Test Ownership

### `internal/cards/cards_test.go`

Cover canonical roots, categories, `README.md`, nested cards, symlinks/non-regular files, malformed YAML/frontmatter, required/optional metadata, all statuses, duplicate IDs across statuses, bounds, H1 title, H2 sections, line ranges, paragraph splits, and hard splits.

### `internal/index/index_test.go`

Cover root-hash default path, deterministic flattening, version/root validation, documents/chunks consistency, malformed/trailing/oversized/unsafe files, permissions, atomic replacement, temp cleanup, and preservation of the previous valid index on failure.

### `internal/search/search_test.go`

Cover query/filter/ID validation, normalization, every score boost, reason order, filter OR/AND composition, path boundaries, package casing, type validation, best chunk/card, final tie ordering, limits, Unicode excerpts, exact get, and internal-field JSON omission.

### `internal/mcpserver/server_test.go`

Use in-memory SDK transports for tool list/schema/annotations, structured-plus-text parity, semantic validation, missing cards, validation-before-reload, failed reload/recovery, size/time/identity fingerprints, and context cancellation.

### `cmd/wfm-rag/main_test.go`

Cover commands, flags, positionals, default/custom index resolution, text/JSON success, stable error codes, exit 2 versus 1, help, and stdout/stderr isolation.

### `cmd/wfm-rag/integration_test.go`

Cover shared realistic corpus expectations, CLI/MCP equivalence, current SDK negotiation, tools/list, both tool calls, schema containment, structured/text parity, live atomic reload, protocol-only stdout, stderr diagnostics, cancellation, and EOF shutdown.

## Fixtures

- Use `t.TempDir()` for mutable cards and indexes.
- Package-local helpers are preferred for focused unit cases.
- Reserve `internal/testdata/corpus` for cross-package retrieval/parity behavior.
- Never mutate the shared fixture in place. Copy it to `t.TempDir()` before reload/edit tests.
- Keep fixtures minimal and readable; assert complete ordering when determinism is the contract.

## Filesystem And Reload Tests

- Avoid sleeps.
- Use `index.Write`/`index.Rebuild` for atomic replacement.
- Use `os.Chtimes` and `os.SameFile` when proving identity-only reload detection.
- Test failure and recovery, not only successful replacement.
- Do not add a filesystem interface merely to force an error if existing file operations can exercise it.

## Protocol Tests

- Use in-memory transport for handler semantics.
- Use a subprocess for real stdio framing/lifecycle.
- Bound reads by bytes and time to prevent hung tests.
- Decode every stdout line as JSON-RPC and reject extra output.
- Keep test-only blocking tools in test subprocesses for deterministic cancellation; do not add production delay hooks.
- Verify startup errors produce no stdout and useful stderr.

## Test Selection

```bash
go test ./internal/cards
go test ./internal/index
go test ./internal/search
go test ./internal/mcpserver
go test ./cmd/wfm-rag
go test -race -count=1 ./...
go vet ./...
```

Run focused tests while iterating, then the complete race suite and vet before handoff.
