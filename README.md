# wfm-rag

`wfm-rag` is a small local retrieval service for curated repository knowledge. A project registry maps short project IDs to canonical repository roots. It reads validated Markdown cards from one project, builds a deterministic JSON index, and exposes the same read-only search and retrieval behavior through a CLI and a stdio MCP server.

This README is the end-to-end onboarding guide. A new developer should be able to install the binary, register a project, create and maintain knowledge, inspect the index, search from a terminal, and connect an LLM client without reading the source first.

## What Changed

Earlier releases located a corpus with a repeatable repository-path flag on every command, merged every configured corpus into one ranked result list, and shared card IDs across those repositories. That design is retired. A local operator registry now maps short project IDs to canonical roots, every command and every MCP tool call selects exactly one project by ID, and all persistent state — registry and indexes — lives in the XDG state directory. See [`docs/planning/project-scoped-knowledgebase.md`](docs/planning/project-scoped-knowledgebase.md) for the full design record.

## What It Does

```text
categorized Markdown cards in one project
        |
        v
discover + validate + chunk
        |
        v
atomic versioned JSON index (one per project)
        |
        +--> CLI search/get, one --project per command
        |
        +--> read-only stdio MCP search/get, one project per call
```

Core properties:

- Markdown under `<target-repository>/.agents/knowledge/` is canonical.
- The JSON index is disposable derived state and contains only active cards.
- Retrieval is deterministic lexical ranking with explainable score reasons.
- CLI and MCP call the same search and exact-retrieval implementation.
- Every retrieval is bound to exactly one named project; the registry resolves the ID to its canonical root, and the index path is derived from that root.
- One MCP process serves the projects allowlisted at launch, up to 64; each call names one of them through a required `project` field.
- MCP cannot write cards, index repositories, run shell commands, or select a project outside its launch allowlist.

## What It Does Not Do

There is **no HTTP server**. `wfm-rag` does not bind a port, expose REST endpoints, provide a URL, or implement Streamable HTTP/SSE MCP. Use an MCP client that can launch a local stdio server.

There is also no:

- database, vector store, embedding model, semantic reranker, or fuzzy search;
- filesystem watcher or automatic reindexing;
- incremental indexing;
- cross-corpus search: one call reads exactly one project, and cross-project questions are separate calls;
- CLI or MCP command for adding, editing, deleting, retiring, or listing cards;
- reading of source files named by card citations.

Card maintenance is intentionally ordinary repository editing followed by an explicit full reindex.

## Requirements

- Go version declared by `go.mod`, currently Go 1.26.5.
- A target repository. `wfm-rag project init <id>` registers it and creates the missing `.agents/knowledge/` skeleton (category directories, `schema.md`, card templates, starter `agent-knowledge` skill) create-only.
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

Register the repository under a short project ID. The registry entry lives in the state directory; `init` additionally scaffolds any missing KB skeleton (schema, templates, starter skill, category directories) in the target repository, create-only:

```bash
wfm-rag project init example --dir "$repo"
```

Build that project's corpus:

```bash
wfm-rag index --project example
```

Search it:

```bash
wfm-rag search --project example "reuse parser"
```

Retrieve the complete Markdown for an exact card ID:

```bash
wfm-rag get --project example example-pattern
```

After any card change, rebuild:

```bash
wfm-rag index --project example
```

An already running MCP process notices the atomic index replacement before its next valid search or get for that project.

Serve the project to an MCP client with an explicit allowlist. The client launches the process; see [`docs/mcp-clients.md`](docs/mcp-clients.md) for configuration files:

```bash
wfm-rag mcp --project example
```

Inspect what the machine knows:

```bash
wfm-rag project list
wfm-rag project healthcheck
```

## Knowledge Storage

Only regular lowercase `.md` files below these exact category directories are candidates:

```text
.agents/knowledge/patterns/
.agents/knowledge/anti-patterns/
.agents/knowledge/dependencies/
.agents/knowledge/incidents/
.agents/knowledge/research/
.agents/knowledge/sources/
.agents/knowledge/state/
```

Nested directories are supported. Files named exactly `README.md`, uncategorized files such as `.agents/knowledge/schema.md`, unknown category directories, symlinks, and non-regular files are not indexed.

The category controls discovery. The frontmatter `type` controls card semantics and filtering. They are validated independently, but the recommended mapping is:

| Card type | Recommended directory |
| --- | --- |
| `pattern` | `patterns/` |
| `anti-pattern` | `anti-patterns/` |
| `dependency` | `dependencies/` |
| `incident` | `incidents/` |
| `research` | `research/` |
| `state` | `state/` |
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

- `id`: stable, unique across active, draft, and retired cards within one project; maximum 256 UTF-8 bytes;
- `type`: one of `pattern`, `anti-pattern`, `dependency`, `incident`, `research`, `state`, `reference`, `decision`;
- `status`: one of `active`, `draft`, `retired`;
- first `# Heading`: title used in results.

Optional fields should still be present so indexing does not report omitted-metadata warnings. Use `[]` only for intentionally empty list fields: `scope`, `triggers`, `packages`, and `paths`. `source_confidence` and `last_verified` are strings. `paths` are citations and ranking hints only; `wfm-rag` never opens the cited files.

Card IDs are unique within one project corpus. The same ID in two different projects is two distinct cards selected by the `project` field.

## Register And Move Projects

```bash
wfm-rag project init api --dir /home/alice/projects/api
wfm-rag project list
wfm-rag project remove api
```

`project init` is idempotent: it registers a new ID, refreshes an ID already bound to the same directory, or rebinds an ID to a new directory and reports the previous root. Rebinding is never implicit; only `init` performs it. After a moved checkout, re-`init` at the new path and rebuild the index, because the derived index path follows the canonical root. After registration, `init` scaffolds the missing knowledge-base skeleton in the target repository — category directories, `schema.md`, card templates, and a starter agent-knowledge skill with the project ID rendered in. Scaffolding is create-only: existing files are never overwritten.

`project remove` deletes only the registry entry; repository and index files are untouched. `project healthcheck [--verify] [--purge]` sweeps registry liveness, index presence, and dangling index directories, reports findings on stdout, and exits 1 when any finding exists.

## Add Knowledge

1. Choose an allowed category.
2. Create a Markdown card with a project-unique stable ID.
3. Use `status: draft` while evidence or wording is incomplete, then change it to `active` when it is ready for retrieval.
4. Add concrete triggers, package names, and repository-relative citation paths that an agent is likely to search.
5. Ensure the card is active and rebuild the index.
6. Dogfood likely queries and retrieve the full card. Draft cards are intentionally absent from search/get.

```bash
wfm-rag index --project example
wfm-rag search --project example "likely developer wording"
wfm-rag get --project example new-card-id
```

A failed build does not replace the previous valid index.

## Update Knowledge

Edit the canonical Markdown, preserve its ID unless identity truly changes, update `last_verified` only after checking the cited evidence, then reindex:

```bash
wfm-rag index --project example
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
wfm-rag index --project example
```

Deleting a card without successfully rebuilding leaves the old card in the existing index.

## Validate Knowledge

There is no separate validate-only command. `index` is the authoritative validation operation. To validate without replacing the normal index, write to an isolated path:

```bash
check_dir="$(mktemp -d)"
wfm-rag index \
  --project example \
  --index "$check_dir/index.json" \
  --format json
```

Remove the temporary directory when finished. `--index` overrides only which file is read or written; the index remains bound to the selected project's canonical root.

## List Knowledge

There is no command or MCP tool that lists cards. A blank search is invalid, and search deliberately excludes zero-score cards. `project list` lists registered projects, not cards.

To list active indexed cards with `jq`, rebuild and inspect the generated index path:

```bash
index_path="$(
  wfm-rag index --project example --format json |
    jq -r '.index'
)"

jq -r '.documents[] | [.id, .type, .source] | @tsv' "$index_path"
```

This lists active cards only. Inventory draft and retired cards from the source tree or version control.

## CLI Reference

Complete usage:

```text
wfm-rag index --project <id> [--index <file>] [--format text|json]
wfm-rag search --project <id> [--index <file>] [--path <path>] [--package <pkg>] [--type <type>] [--limit 5] [--format text|json] <query>
wfm-rag get --project <id> [--index <file>] [--format text|json] <card-id>
wfm-rag mcp --project <id> [--project <id> ...] | --all-projects [--format text|json]
wfm-rag project init <id> [--dir <path>] [--format text|json]
wfm-rag project remove <id> [--format text|json]
wfm-rag project list [--format text|json]
wfm-rag project healthcheck [--verify] [--purge] [--format text|json]
```

Put flags before the positional query or card ID. Quote multiword queries. `--project` is required on `index`, `search`, and `get` and is the sole locator of a corpus; `mcp` takes a repeatable `--project` allowlist (at most 64) or `--all-projects`.

### Index

```bash
wfm-rag index --project example
wfm-rag index --project example --format json
wfm-rag index --project example --index /absolute/path/index.json
```

`index` performs a complete build and atomic replacement. It reports active cards, skipped cards, chunks, source bytes, warnings, the output path, and generation time.

### Search

```bash
wfm-rag search --project example "pointer helper"

wfm-rag search \
  --project example \
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
wfm-rag get --project example ini-list-parsing
wfm-rag get --project example --format json ini-list-parsing
```

IDs are exact and case-sensitive. Get returns the complete original indexed Markdown, including frontmatter, plus source and citations.

### Output And Exit Codes

- Text is the default for `index`, `search`, `get`, and the `project` subcommands.
- `--format json` writes structured success output to stdout.
- Errors always go to stderr.
- Exit `0`: success, including no search results and a clean `project healthcheck`.
- Exit `2`: invalid command, flags, query, filters, limit, format, ID, or project selection.
- Exit `1`: operational failure such as missing/invalid index, malformed corpus, registry problem, or a `project healthcheck` that reported findings.

Stable JSON error codes are documented in [`docs/cli.md`](docs/cli.md).

## State Directory

All persistent state lives in `$XDG_DATA_HOME/wfm-rag` when `$XDG_DATA_HOME` is set and absolute; a relative or empty value is invalid per the XDG Base Directory spec and falls back to `$HOME/.local/share/wfm-rag`. The directory is created `0700` and holds:

```text
projects.json                      registry: id -> canonical root, mode 0600
projects.lock                      flock writer lock, mode 0600
<root-sha256>/index.json           each project's derived index
```

The hash is the full lowercase SHA-256 digest of the canonical absolute repository root after resolving root symlinks. Moving a checkout changes its default index location; re-`init` the project at the new path and rebuild.

An explicit `--index` path overrides only which file is read or written; the index remains bound to the selected project's canonical root. Pass the same path to every producer:

```bash
index=/absolute/path/to/index.json

wfm-rag index --project example --index "$index"
wfm-rag search --project example --index "$index" "query"
wfm-rag get --project example --index "$index" card-id
```

`mcp` does not accept `--index`; served projects always use their derived paths.

The index contains complete card Markdown and is created with mode `0600`. See [`docs/index-format.md`](docs/index-format.md) for format and persistence guarantees.

## Connect An LLM Through MCP

Build the project's index first. The client launches the stdio process; do not start a second standalone process.

Generic configuration:

```json
{
  "mcpServers": {
    "agent-knowledge": {
      "command": "/home/alice/.local/bin/wfm-rag",
      "args": [
        "mcp",
        "--project",
        "example"
      ]
    }
  }
}
```

The server exposes exactly:

- `agent_knowledge_search`: required `project` plus `query` and optional `paths`, `packages`, `types`, and `limit`;
- `agent_knowledge_get`: required `project` plus the exact card `id`.

Both inputs are closed-world (`additionalProperties: false`) and constrain `project` to a sorted enum of the IDs allowlisted at launch. Inputs cannot contain a repository path, index path, write operation, or shell command. Registry membership is fixed at launch: `project init` and `project remove` change a running server only after the client restarts it, while rebuilt index content reloads live. A configured project with a missing or invalid index degrades to a stderr startup warning and an actionable per-call error naming the repair command; healthy projects keep serving.

Full OpenCode, OMP (Oh My Pi), generic-client, multi-project, tool-schema, degraded-project, and client-specific troubleshooting instructions are in [`docs/mcp-clients.md`](docs/mcp-clients.md).

## HTTP Server

There is no HTTP launch command. Running:

```bash
wfm-rag mcp --project example
```

starts a newline-delimited JSON-RPC server on stdin/stdout. It does not listen on localhost and will appear to wait when launched manually because it expects an MCP client to write protocol messages to stdin.

HTTP transport is deferred work. Do not place `wfm-rag` behind an ad hoc wrapper that writes banners or logs to stdout; stdout must contain MCP protocol messages only.

## Portable Agent Skill

The canonical copy-ready authoring and retrieval skill is:

```text
.agents/skills/agent-knowledge/SKILL.md
```

Copy it into another corpus-bearing repository, route knowledge retrieval and lifecycle tasks to it from that repository's `AGENTS.md`, and append only a small repository profile:

```bash
rag=/absolute/path/to/wfm-rag
target=/absolute/path/to/target-repository

mkdir -p "$target/.agents/skills/agent-knowledge"
cp "$rag/.agents/skills/agent-knowledge/SKILL.md" \
  "$target/.agents/skills/agent-knowledge/SKILL.md"
```

Add a routing row equivalent to:

```markdown
| Search, get, add, update, retire, delete, validate, reindex, or evaluate knowledge cards | `.agents/skills/agent-knowledge/SKILL.md` |
```

`project init` scaffolds this starter skill automatically when the target repository does not have one yet. The default skill expects `wfm-rag` on `PATH`, uses the registered project ID, and the project's derived index. Customize only the profile when a repository has a custom binary/index path, project ID, MCP server name, retrieval triggers, evidence requirements, or test workflow. Restart the agent client after copying or editing the skill.

## Troubleshooting

Common cases:

- **`project_unknown`**: the ID is not registered. Register it with `project init <id> --dir <path>` or check `wfm-rag project list`; the error message lists the registered IDs.
- **`index_missing`**: run `wfm-rag index --project <id>` as the same OS user that runs the search or starts the client.
- **`index belongs to another root`**: rebuild after moving a checkout, re-`init` the project at the new path, and stop sharing one custom index across projects.
- **card edit not visible**: re-run `index`; there is no watcher.
- **retired/deleted card still visible**: the old index is still active because reindexing was not run or failed.
- **MCP process looks hung**: it is a stdio server waiting for protocol input; let the MCP client launch it.
- **MCP has no tools**: test CLI search/get first, use an absolute binary path, and inspect client-captured stderr.
- **MCP tool enum is stale after `project init` or `project remove`**: expected. Registry membership is fixed at launch; restart the client.
- **`get` not found**: IDs are case-sensitive and only active cards are present in the index.
- **duplicate ID after retirement**: IDs must remain unique across every status within one project.
- **unexpected path-filter miss**: filters compare frontmatter citations, not card source paths or filesystem contents.
- **reload failure**: the changed index for the selected project is invalid or root-mismatched; stale in-memory data is intentionally not served, and other projects are unaffected.

See [`docs/troubleshooting.md`](docs/troubleshooting.md) for detailed diagnosis.

## Development

Repository packages:

| Path | Responsibility |
| --- | --- |
| `cmd/wfm-rag` | CLI flags, project resolution, output, error classification, process startup, stdio integration tests |
| `internal/cards` | Root/corpus discovery, YAML frontmatter, validation, Markdown chunking |
| `internal/index` | Versioned JSON model, structural validation, default path, atomic writes, loading |
| `internal/project` | Project registry file, flock persistence, ID rules, statuses, healthcheck sweep |
| `internal/search` | Query validation, filters, deterministic scoring, reasons, excerpts, exact get |
| `internal/mcpserver` | Typed read-only MCP tools, per-project index reload, cancellation, stdio server |
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
- [`docs/planning/project-scoped-knowledgebase.md`](docs/planning/project-scoped-knowledgebase.md): implemented project registry, state directory, and per-project retrieval design.

## Project Status

The CLI, deterministic index/search core, project registry with healthcheck maintenance, read-only stdio MCP server, integration tests, live API-corpus dogfood, and client configuration are implemented. This remains internal local research software and is not intended for public distribution.
