---
name: go-test-writing
description: Use when adding, changing, repairing, or reviewing Go tests, corpus fixtures, ranking assertions, filesystem safety tests, or MCP protocol tests in wfm-rag.
---

# Go Test Writing

Tests are required for changed behavior. Prefer standard library `testing` and table-driven cases; do not add a test framework without a concrete need.

## Test Boundaries

- `internal/cards`: malformed YAML, metadata validation, statuses, duplicate IDs, deterministic discovery, symlinks, file limits, and chunk boundaries.
- `internal/index`: versions, canonical roots, permissions, atomic replacement, malformed JSON, and failed rebuild preservation.
- `internal/search`: normalization, every score boost, filters, tie-breaking, deduplication by card, limits, and excerpts.
- `internal/mcpserver`: typed validation, equivalent core results, reload behavior, cancellation, and stdio purity.

## Practices

- Use `t.TempDir()` for files and indexes.
- Keep test fixtures small and self-contained under `internal/testdata`.
- Assert complete ordering where determinism is the contract.
- Test failures as well as successful retrieval.
- Avoid sleeps for reload tests; update files atomically and control timestamps or injectable file checks only if necessary.
- Prefer direct function tests, then add a small number of integration and subprocess tests.

## Verification

```bash
go test ./internal/<package>
go test -race -count=1 ./...
go vet ./...
```
