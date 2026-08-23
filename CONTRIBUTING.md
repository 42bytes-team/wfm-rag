# Contributing

## Scope

`wfm-rag` is a small deterministic local tool. Preserve the simplest design that satisfies measured retrieval and operational requirements.

Do not add HTTP transport, databases, embeddings, watchers, background workers, multi-root routing, dependency injection, or write-capable MCP tools without an explicit requirement and design review.

## Read First

- `README.md`: user and operator contract.
- `docs/architecture.md`: package ownership and invariants.
- `docs/knowledge-authoring.md`: source corpus contract.
- `docs/index-format.md`: persisted format.
- `docs/mcp-clients.md`: client compatibility.
- `docs/planning/agent-rag-mcp-implementation.md`: implemented v0.1 design record.

## Repository Map

| Area | Implementation | Focused tests |
| --- | --- | --- |
| CLI/output/errors/startup | `cmd/wfm-rag/main.go` | `cmd/wfm-rag/main_test.go` |
| CLI/MCP parity and stdio | `cmd/wfm-rag`, shared `internal/search` core | `cmd/wfm-rag/integration_test.go` |
| Cards/chunking | `internal/cards/cards.go` | `internal/cards/cards_test.go` |
| Index/persistence | `internal/index/index.go` | `internal/index/index_test.go` |
| Search/get | `internal/search/search.go` | `internal/search/search_test.go` |
| MCP/reload | `internal/mcpserver/server.go` | `internal/mcpserver/server_test.go` |

## Change Workflow

1. Identify the package that owns the behavior.
2. Read its focused tests and only the relevant design section.
3. Trace both CLI and MCP when shared retrieval behavior changes.
4. Add a failing observable-contract test or important failure-path test.
5. Make the smallest coherent change.
6. Update user docs when commands, schemas, outputs, limits, or operations change.
7. Review for accidental protocol output and nondeterministic ordering.
8. Run full verification.

## Required Verification

```bash
gofmt -w cmd/wfm-rag/*.go internal/*/*.go
go test -race -count=1 ./...
go vet ./...
git diff --check
```

After changing `go.mod` or `go.sum`, also run:

```bash
go mod verify
```

No external services are needed.

Focused commands:

```bash
go test ./internal/cards
go test ./internal/index
go test ./internal/search
go test ./internal/mcpserver
go test ./cmd/wfm-rag
```

## Behavioral Changes

### Cards Or Chunking

- Keep filesystem containment and bounds.
- Preserve deterministic discovery order.
- Test all statuses and duplicate IDs.
- If serialized metadata/chunks change, assess index version compatibility.

### Index Format

- Decide explicitly whether `index.Version` must increase.
- Keep old-index preservation on failed builds/writes.
- Update `docs/index-format.md`.
- Test malformed, root-mismatched, unsupported, unsafe, and oversized input.

### Ranking Or Filters

- Record a concrete failed retrieval case before changing weights.
- Add the test first.
- Change only `internal/search`; never duplicate ranking in CLI/MCP.
- Preserve reason order and deterministic ties.
- Run CLI/MCP parity and dogfood representative queries.

### CLI

- Preserve stdout/stderr separation.
- Preserve stable JSON error codes and exit codes.
- Put reusable behavior in the owning internal package.

### MCP

- Verify API signatures against the SDK version pinned in `go.mod`.
- Keep exactly two read-only tools unless scope explicitly changes.
- Keep root/index selection at process startup, never tool input.
- Preserve structured output and JSON text fallback.
- Preserve validation-before-reload and no-stale-on-reload-failure behavior.
- Run in-memory and real subprocess protocol tests.

## Documentation Changes

When implementation behavior changes, update all affected contracts:

- command/flags/errors: `README.md`, `docs/cli.md`;
- card schema/lifecycle/limits: `README.md`, `docs/knowledge-authoring.md`;
- package/data flow/invariants: `docs/architecture.md`, `AGENTS.md`, relevant skill;
- index fields/version/location: `docs/index-format.md`;
- MCP configuration/compatibility: `docs/mcp-clients.md`;
- operational diagnosis: `docs/troubleshooting.md`.

Do not treat planning documents as the only user documentation.

## Manual Smoke Test

```bash
mkdir -p bin
go build -o ./bin/wfm-rag ./cmd/wfm-rag

repo=/absolute/path/to/repository
./bin/wfm-rag index --root "$repo"
./bin/wfm-rag search --root "$repo" "known query"
./bin/wfm-rag get --root "$repo" known-card-id
```

Use a configured MCP client for MCP smoke tests. Launching `mcp` manually waits for protocol input and does not start HTTP.

## Git

Do not stage generated indexes, temporary indexes, binaries, or changes made in a separate target repository unless they are intentionally part of that repository's work. Keep RAG implementation changes separate from target knowledge-corpus changes.
