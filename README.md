# wfm-rag

`wfm-rag` is a small local retrieval service for curated repository knowledge. It reads validated Markdown cards from a repository, builds a deterministic JSON index, and exposes the same read-only search and retrieval behavior through a CLI and a stdio MCP server.

This README is the end-to-end onboarding guide. A new developer should be able to install the binary, create and maintain knowledge, inspect the index, search from a terminal, and connect an LLM client without reading the source first.

## What It Does

```text
categorized Markdown cards
        |
        v
discover + validate + chunk
        |
        v
atomic versioned JSON index
        |
        +--> CLI search/get
        |
        +--> read-only stdio MCP search/get
```

Core properties:

- Markdown under `<target-repository>/.agents/knowledge/` is canonical.
- The JSON index is disposable derived state and contains only active cards.
- Retrieval is deterministic lexical ranking with explainable score reasons.
- CLI and MCP call the same search and exact-retrieval implementation.
- One MCP process is fixed to one repository root selected at startup.
- MCP cannot write cards, index repositories, run shell commands, or select another root.

## What It Does Not Do

There is **no HTTP server** in v0.1. `wfm-rag` does not bind a port, expose REST endpoints, provide a URL, or implement Streamable HTTP/SSE MCP. Use an MCP client that can launch a local stdio server.

There is also no:

- database, vector store, embedding model, semantic reranker, or fuzzy search;
- filesystem watcher or automatic reindexing;
- incremental indexing;
- multi-repository MCP process;
- CLI or MCP command for adding, editing, deleting, retiring, or listing cards;
- reading of source files named by card citations.

Card maintenance is intentionally ordinary repository editing followed by an explicit full reindex.

## Requirements

- Go version declared by `go.mod`, currently Go 1.26.5.
- A target repository containing a real `.agents/knowledge/` directory.
- An MCP-capable client for LLM integration. OpenCode, OMP, and generic clients are documented in [`docs/mcp-clients.md`](docs/mcp-clients.md).
- Optional: `jq` for inspecting JSON output and listing indexed cards.

No Docker containers, databases, queues, Makefile, or external services are required.

## Install

Run from the `wfm-rag` repository root:

```bash
go version
GOBIN="$HOME/.local/bin" go install ./cmd/wfm-rag
test -x "$HOME/.local/bin/wfm-rag"
"$HOME/.local/bin/wfm-rag" help
```

Use the absolute binary path in GUI client configuration. GUI applications often do not inherit the same `PATH` as a terminal.

For repository-local development instead:

```bash
mkdir -p bin
go build -o ./bin/wfm-rag ./cmd/wfm-rag
./bin/wfm-rag help
```

All remaining examples use `wfm-rag` for readability. Replace it with the absolute installed path when it is not in `PATH`.

## Quick Start

To try the complete workflow without modifying another repository, create a temporary target repository and one active card:

```bash
repo="$(mktemp -d)"
mkdir -p "$repo/.agents/knowledge/patterns"

cat >"$repo/.agents/knowledge/patterns/example-pattern.md" <<'EOF'
---
id: example-pattern
type: pattern
status: active
scope:
  - onboarding
triggers:
  - reuse parser
packages: []
paths:
  - src/example.go
source_confidence: high
last_verified: 2026-08-23
---

# Example Pattern

## Guidance

Reuse the existing parser before writing a replacement.
EOF
```

That creates the minimum valid layout:

```text
repository/
  .agents/
    knowledge/
      patterns/
        example-pattern.md
```

Build the index:

```bash
wfm-rag index --root "$repo"
```

Search it:

```bash
wfm-rag search --root "$repo" "reuse parser"
```

Retrieve the complete Markdown for an exact card ID:

```bash
wfm-rag get --root "$repo" example-pattern
```

After any card change, rebuild:

```bash
wfm-rag index --root "$repo"
```

An already running MCP process notices the atomic index replacement before its next valid search or get operation.

## Knowledge Storage

Only regular lowercase `.md` files below these exact category directories are candidates:

```text
.agents/knowledge/patterns/
.agents/knowledge/anti-patterns/
.agents/knowledge/dependencies/
.agents/knowledge/incidents/
.agents/knowledge/sources/
```

Nested directories are supported. Files named exactly `README.md`, uncategorized files such as `.agents/knowledge/schema.md`, unknown category directories, symlinks, and non-regular files are not indexed.

The category controls discovery. The frontmatter `type` controls card semantics and filtering. They are validated independently, but the recommended mapping is:

| Card type | Recommended directory |
| --- | --- |
| `pattern` | `patterns/` |
| `anti-pattern` | `anti-patterns/` |
| `dependency` | `dependencies/` |
| `incident` | `incidents/` |
| `reference`, `decision` | `sources/` |

See [`docs/knowledge-authoring.md`](docs/knowledge-authoring.md) for the complete schema, lifecycle, limits, and writing guidance.

## Card Format

Every card requires YAML frontmatter and a level-one Markdown heading:

```markdown
---
id: ini-list-parsing
type: dependency
status: active
scope:
  - go
  - config
triggers:
  - INI
  - comma-separated
packages:
  - gopkg.in/ini.v1
paths:
  - src/pkgs/config/ini.go
source_confidence: high
last_verified: 2026-08-23
---

# INI List Parsing

## Use When

Use this card when a known INI key contains a delimiter-separated list.

## Guidance

Read the exact section and key, then use `Key.Strings(",")`.

## Verify

- Read the cited implementation.
- Run the relevant tests.
```

Required fields:

- `id`: stable, unique across active, draft, and retired cards; maximum 256 UTF-8 bytes;
- `type`: one of `pattern`, `anti-pattern`, `dependency`, `incident`, `reference`, `decision`;
- `status`: one of `active`, `draft`, `retired`;
- first `# Heading`: title used in results.

Optional fields should still be present so indexing does not report omitted-metadata warnings. Use `[]` only for intentionally empty list fields: `scope`, `triggers`, `packages`, and `paths`. `source_confidence` and `last_verified` are strings. `paths` are citations and ranking hints only; `wfm-rag` never opens the cited files.

## Add Knowledge

1. Choose an allowed category.
2. Create a Markdown card with a repository-unique stable ID.
3. Use `status: draft` while evidence or wording is incomplete, then change it to `active` when it is ready for retrieval.
4. Add concrete triggers, package names, and repository-relative citation paths that an agent is likely to search.
5. Ensure the card is active and rebuild the index.
6. Dogfood likely queries and retrieve the full card. Draft cards are intentionally absent from search/get.

```bash
wfm-rag index --root "$repo"
wfm-rag search --root "$repo" "likely developer wording"
wfm-rag get --root "$repo" new-card-id
```

A failed build does not replace the previous valid index.

## Update Knowledge

Edit the canonical Markdown, preserve its ID unless identity truly changes, update `last_verified` only after checking the cited evidence, then reindex:

```bash
wfm-rag index --root "$repo"
```

Editing Markdown without reindexing has no effect on CLI or MCP results.

## Retire Knowledge

Retirement preserves history and reserves the ID:

```yaml
status: retired
```

Reindex afterward. The command reports a skipped-card warning, and the retired card is absent from the new search/get index. Draft and retired cards still must parse correctly and still participate in duplicate-ID validation.

## Delete Knowledge

Delete the source card through the target repository's normal version-control workflow, then reindex:

```bash
git -C "$repo" rm .agents/knowledge/patterns/obsolete-card.md
wfm-rag index --root "$repo"
```

Deleting a card without successfully rebuilding leaves the old card in the existing index.

## Validate Knowledge

There is no separate validate-only command. `index` is the authoritative validation operation. To validate without replacing the normal index, write to an isolated path:

```bash
check_dir="$(mktemp -d)"
wfm-rag index \
  --root "$repo" \
  --index "$check_dir/index.json" \
  --format json
```

Remove the temporary directory when finished.

## List Knowledge

There is no `list` command or MCP list-cards tool. A blank search is invalid, and search deliberately excludes zero-score cards.

To list active indexed cards with `jq`, rebuild and inspect the generated index path:

```bash
index_path="$(
  wfm-rag index --root "$repo" --format json |
    jq -r '.index'
)"

jq -r '.documents[] | [.id, .type, .source] | @tsv' "$index_path"
```

This lists active cards only. Inventory draft and retired cards from the source tree or version control.

## CLI Reference

Complete usage:

```text
wfm-rag index --root <repo> [--index <file>] [--format text|json]
wfm-rag search --root <repo> [--index <file>] [--path <path>] [--package <pkg>] [--type <type>] [--limit 5] [--format text|json] <query>
wfm-rag get --root <repo> [--index <file>] [--format text|json] <card-id>
wfm-rag mcp --root <repo> [--index <file>]
```

Put flags before the positional query or card ID. Quote multiword queries.

### Index

```bash
wfm-rag index --root "$repo"
wfm-rag index --root "$repo" --format json
wfm-rag index --root "$repo" --index /absolute/path/index.json
```

`index` performs a complete build and atomic replacement. It reports active cards, skipped cards, chunks, source bytes, warnings, the output path, and generation time.

### Search

```bash
wfm-rag search --root "$repo" "pointer helper"

wfm-rag search \
  --root "$repo" \
  --path src/pkgs/config/ini.go \
  --package gopkg.in/ini.v1 \
  --type dependency \
  --limit 10 \
  --format json \
  "config parser"
```

`--path`, `--package`, and `--type` are repeatable. Values within one filter kind are ORed; different filter kinds are ANDed. The default limit is 5 and the allowed range is 1 through 20. No matches is a successful result, not an error.

Search is lexical. It lowercases and tokenizes Unicode letters/numbers, but does not stem or expand synonyms. `parse`, `parser`, and `parsing` are distinct terms.

### Get

```bash
wfm-rag get --root "$repo" ini-list-parsing
wfm-rag get --root "$repo" --format json ini-list-parsing
```

IDs are exact and case-sensitive. Get returns the complete original indexed Markdown, including frontmatter, plus source and citations.

### Output And Exit Codes

- Text is the default for `index`, `search`, and `get`.
- `--format json` writes structured success output to stdout.
- Errors always go to stderr.
- Exit `0`: success, including no search results.
- Exit `2`: invalid command, flags, query, filters, limit, format, or ID.
- Exit `1`: operational failure such as missing/invalid index, malformed corpus, or filesystem failure.

Stable JSON error codes are documented in [`docs/cli.md`](docs/cli.md).

## Index Location

Without `--index`, the path is:

```text
~/.local/share/wfm-rag/<root-sha256>/index.json
```

The hash is the full lowercase SHA-256 digest of the canonical absolute repository root after resolving root symlinks. Moving a checkout changes its default index location. `$XDG_DATA_HOME` is not consulted.

If using `--index`, pass the same path to every producer and consumer:

```bash
index=/absolute/path/to/index.json

wfm-rag index --root "$repo" --index "$index"
wfm-rag search --root "$repo" --index "$index" "query"
wfm-rag get --root "$repo" --index "$index" card-id
wfm-rag mcp --root "$repo" --index "$index"
```

The index contains complete card Markdown and is created with mode `0600`. See [`docs/index-format.md`](docs/index-format.md) for format and persistence guarantees.

## Connect An LLM Through MCP

Build the index first. The client launches the stdio process; do not start a second standalone process.

Generic configuration:

```json
{
  "mcpServers": {
    "agent-knowledge": {
      "command": "/home/alice/.local/bin/wfm-rag",
      "args": [
        "mcp",
        "--root",
        "/home/alice/projects/api"
      ]
    }
  }
}
```

The server exposes exactly:

- `agent_knowledge_search`: query plus optional `paths`, `packages`, `types`, and `limit`;
- `agent_knowledge_get`: exact card `id`.

Tool inputs cannot contain a root, project, index path, write operation, or shell command.

Full OpenCode, OMP (Oh My Pi), generic-client, multi-repository, validation, and client-specific troubleshooting instructions are in [`docs/mcp-clients.md`](docs/mcp-clients.md).

## HTTP Server

There is no HTTP launch command. Running:

```bash
wfm-rag mcp --root "$repo"
```

starts a newline-delimited JSON-RPC server on stdin/stdout. It does not listen on localhost and will appear to wait when launched manually because it expects an MCP client to write protocol messages to stdin.

HTTP transport is deferred work. Do not place `wfm-rag` behind an ad hoc wrapper that writes banners or logs to stdout; stdout must contain MCP protocol messages only.

## Troubleshooting

Common cases:

- **`index missing`**: run `index` with the same root, OS user, and optional `--index` path as the consumer.
- **`index belongs to another root`**: rebuild after moving a checkout or stop sharing one custom index across repositories.
- **card edit not visible**: re-run `index`; there is no watcher.
- **retired/deleted card still visible**: the old index is still active because reindexing was not run or failed.
- **MCP process looks hung**: it is a stdio server waiting for protocol input; let the MCP client launch it.
- **MCP has no tools**: test CLI search/get first, use an absolute binary path, and inspect client-captured stderr.
- **`get` not found**: IDs are case-sensitive and only active cards are present in the index.
- **duplicate ID after retirement**: IDs must remain unique across every status.
- **unexpected path-filter miss**: filters compare frontmatter citations, not card source paths or filesystem contents.
- **reload failure**: the changed index is invalid or root-mismatched; stale in-memory data is intentionally not served.

See [`docs/troubleshooting.md`](docs/troubleshooting.md) for detailed diagnosis.

## Development

Repository packages:

| Path | Responsibility |
| --- | --- |
| `cmd/wfm-rag` | CLI flags, output, error classification, process startup, stdio integration tests |
| `internal/cards` | Root/corpus discovery, YAML frontmatter, validation, Markdown chunking |
| `internal/index` | Versioned JSON model, structural validation, default path, atomic writes, loading |
| `internal/search` | Query validation, filters, deterministic scoring, reasons, excerpts, exact get |
| `internal/mcpserver` | Typed read-only MCP tools, stable index reload, cancellation, stdio server |
| `internal/testdata` | Shared realistic integration corpus |

Required checks after Go changes:

```bash
gofmt -w cmd/wfm-rag/*.go internal/*/*.go
go test -race -count=1 ./...
go vet ./...
```

There are no external test services. See [`CONTRIBUTING.md`](CONTRIBUTING.md) and [`docs/architecture.md`](docs/architecture.md) before changing behavior.

## Documentation Map

- [`docs/cli.md`](docs/cli.md): complete command, output, validation, and error reference.
- [`docs/knowledge-authoring.md`](docs/knowledge-authoring.md): schema and add/update/draft/retire/delete/list/validate lifecycle.
- [`docs/mcp-clients.md`](docs/mcp-clients.md): OpenCode, OMP, and generic stdio setup.
- [`docs/architecture.md`](docs/architecture.md): package ownership, data flow, invariants, ranking, and freshness.
- [`docs/index-format.md`](docs/index-format.md): persisted JSON contract and atomic replacement.
- [`docs/troubleshooting.md`](docs/troubleshooting.md): symptom-based operational diagnosis.
- [`CONTRIBUTING.md`](CONTRIBUTING.md): development workflow and verification.
- [`docs/planning/agent-rag-mcp-implementation.md`](docs/planning/agent-rag-mcp-implementation.md): implemented v0.1 design record.

## Project Status

The v0.1 CLI, deterministic index/search core, read-only stdio MCP server, integration tests, live API-corpus dogfood, and client configuration are implemented. This remains internal local research software and is not intended for public distribution.
