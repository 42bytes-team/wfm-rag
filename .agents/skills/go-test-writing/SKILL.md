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

Cover the state-directory rule (absolute XDG wins, unset/empty/relative fallback, empty HOME), root-hash default path including `DefaultPathFromCanonical` on a vanished root, deterministic flattening, version/root validation, documents/chunks consistency, malformed/trailing/oversized/unsafe files, permissions, atomic replacement, temp cleanup, and preservation of the previous valid index on failure.

### `internal/search/search_test.go`

Cover query/filter/ID validation, project-envelope validation (empty project rejected, project echoed in search/get JSON), normalization, every score boost, reason order, filter OR/AND composition, path boundaries, package casing, type validation, best chunk/card, final score/source/ordinal tie ordering, limits, Unicode excerpts, exact get, and internal-field JSON omission.

### `internal/project/project_test.go`

Cover state-directory resolution, registry round-trip with `0600`/`0700` modes, symlink/non-regular/oversized rejection, trailing data, duplicate keys, unknown fields, non-absolute stored roots, sorted rendering, failed-write preservation, `flock` contention with `ErrLocked`, killed-writer lock release, concurrent `init`/`remove` losing no update, slug acceptance/rejection, the 64-project cap, vanished-root repair, statuses without opening index files, healthcheck finding kinds and ordering, and purge safety (only single-regular-`index.json` dangling directories removed; claimed paths untouched).

### `internal/scaffold/scaffold_test.go`

Cover the full skeleton for a fresh repository with the project ID rendered into the starter skill and the knowledge README, the scaffolded knowledge README carrying the profile-filling prompt, idempotence with no overwrite of existing files or symlinks, filling only the missing pieces of a partial skeleton, the returned repository-relative paths, rejection of unsafe skeleton paths (a symlinked intermediate directory or a wrong-kind file destination is reported, never displaced or written through), profile-state detection (placeholders present, profile filled, customized README without the scaffolded prompt), and the required project ID.

### `internal/mcpserver/server_test.go`

Use in-memory SDK transports for tool list/schema/annotations, the launch-scoped sorted `project` enum, closed-world input rejection, project isolation (a call cannot return another project's cards; a shared card ID resolves per selected project), degraded startup warnings with live recovery and no restart, per-project reload isolation and failure, structured-plus-text parity, semantic validation, validation-before-reload, size/time/identity fingerprints, and context cancellation.

### `cmd/wfm-rag/main_test.go`

Cover commands, flags, positionals, the `project` subcommand group, registry-backed project resolution and its error codes, `--index` location overrides, default/derived index resolution, text/JSON success, stable error codes, exit 2 versus 1, help, and stdout/stderr isolation.

### `cmd/wfm-rag/integration_test.go`

Cover shared realistic corpus expectations, CLI/MCP equivalence, the fresh-clone flow (`project init`, `index`, retrieval), current SDK negotiation, tools/list, both tool calls with explicit projects, schema containment, structured/text parity, live atomic reload, degraded startup with stderr warning and recovery without restart, protocol-only stdout, stderr diagnostics, cancellation, and EOF shutdown. Subprocess tests isolate `HOME`/`XDG_DATA_HOME` into `t.TempDir()`.

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
go test ./internal/project
go test ./internal/mcpserver
go test -race -count=1 ./...
go vet ./...
```

Run focused tests while iterating, then the complete race suite and vet before handoff.
