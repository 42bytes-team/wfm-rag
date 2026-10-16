# wfm-rag

`wfm-rag` is a local retrieval service for curated repository knowledge. A project registry maps short project IDs to canonical repository roots. The tool reads validated Markdown cards from one project, builds a deterministic JSON index, and serves the same read-only search and retrieval through a CLI and a stdio MCP server.

This README covers installation, onboarding, client setup, the command reference, and a short tour of the knowledge base. Detailed contracts live under [`docs/`](docs/).

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

- Markdown under `<target-repository>/.agents/knowledge/` is canonical. The JSON index is derived state and holds only active cards.
- Ranking is lexical and deterministic. Every result carries its score reasons.
- CLI and MCP call the same search and exact-retrieval implementation.
- One call reads one project. The registry resolves the ID to a canonical root, and the index path derives from that root.
- One MCP process serves the projects allowlisted at launch, up to 64. Each call names one of them through a required `project` field.

## What It Does Not Do

There is no HTTP server. `wfm-rag` binds no port, exposes no URL, and implements no Streamable HTTP or SSE transport. An MCP client must launch the stdio process.

There is also no:

- database, vector store, embedding model, semantic reranker, or fuzzy search;
- filesystem watcher or incremental indexing;
- cross-corpus search. A cross-project question is two calls that name each project;
- command or tool that adds, edits, retires, deletes, or lists cards;
- reading of source files named by card citations.

Card maintenance is ordinary file editing plus an explicit `index` run.

## Install

Run from the `wfm-rag` repository root. `go.mod` declares Go 1.26.5.

```bash
GOBIN="$HOME/.local/bin" go install ./cmd/wfm-rag
"$HOME/.local/bin/wfm-rag" help
```

For repository-local development instead:

```bash
go build -o ./bin/wfm-rag ./cmd/wfm-rag
```

Configure the absolute binary path in GUI client configuration. A GUI application often does not inherit the terminal `PATH`. No Docker, database, queue, message broker, or Makefile is involved. `jq` is optional and only helps to inspect JSON output.

The examples below assume `wfm-rag` is on `PATH`. Replace it with the absolute path when it is not.

## Onboarding

Run these three commands from the repository you want to make searchable:

```bash
cd /home/alice/projects/api
wfm-rag project init api
wfm-rag index --project api
wfm-rag search --project api "config parser"
```

`project init` registers the project and creates every missing part of the knowledge base in the target repository: the category directories, `.agents/knowledge/schema.md`, the card templates, and a starter `agent-knowledge` skill with the project ID rendered in. Scaffolding is create-only. Existing files are never overwritten, so a customized repository keeps its own copies.

`project init` uses the working directory. Pass `--dir /home/alice/projects/api` to register another path. The command is idempotent: a new ID registers, the same directory refreshes, and a different directory rebinds the ID and reports the previous root.

Continue with these commands:

```bash
wfm-rag get --project api ini-list-parsing   # complete card by exact ID
wfm-rag project list                         # registered projects + index status
wfm-rag project healthcheck                  # whole-state sweep
```

Re-run `wfm-rag index --project api` after every card edit. An already running MCP process reloads that project's index before its next valid call.

`project list` reports `root missing` or `index missing` per project. `project healthcheck` checks registry liveness, index presence, and dangling index directories. Add `--verify` to run the full index load for each project, and `--purge` to delete a dangling directory that holds exactly one regular `index.json`. The command prints findings on stdout and exits 1 when any finding exists.

After you move a checkout, run `project init api --dir <new path>` and rebuild. The derived index path follows the canonical root, so the old snapshot becomes unreachable by design. `project remove api` deletes only the registry entry. Repository files and index files stay in place.

### State Directory

Persistent state lives in `$XDG_DATA_HOME/wfm-rag` when `$XDG_DATA_HOME` is set and absolute. An empty or relative value falls back to `$HOME/.local/share/wfm-rag`. The directory is created `0700` and holds:

```text
projects.json                    registry: id -> canonical root, mode 0600
projects.lock                    flock writer lock, mode 0600
<root-sha256>/index.json         each project's derived index, mode 0600
```

Run the client and the indexing commands as the same OS user, because this data is user-owned.

## Connect An LLM Through MCP

The client launches the process. Do not start a second standalone server. One entry with a repeated `--project` allowlist publishes one tool pair for every served project, while separate entries repeat both tool definitions in every model request. `mcp` accepts no `--index`: every served project uses its derived index path.

### OpenCode

Project `opencode.json`. OpenCode V2 places servers under `mcp.servers`, not directly under `mcp`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "api-agent-knowledge": {
        "type": "local",
        "command": ["/home/alice/.local/bin/wfm-rag", "mcp", "--project", "api"]
      }
    }
  }
}
```

Or let OpenCode write the entry, then confirm the connection:

```bash
opencode mcp add api-agent-knowledge -- /home/alice/.local/bin/wfm-rag mcp --project api
opencode mcp list
```

`type: "local"` and the full `command` array are required. `disabled: true` keeps an entry configured but unconnected, and `environment`, not `env`, sets process variables. `codemode` defaults to `true`, which exposes MCP tools through Code Mode. Set it to `false` to expose `agent_knowledge_search` and `agent_knowledge_get` directly. Restart OpenCode after a manual edit, because it loads configuration at startup.

### OMP (Oh My Pi)

Project `.omp/mcp.json`, or user-level `~/.omp/agent/mcp.json`. Project configuration is keyed to the working directory, so start OMP from the repository root.

```json
{
  "$schema": "https://raw.githubusercontent.com/can1357/oh-my-pi/main/packages/coding-agent/src/config/mcp-schema.json",
  "mcpServers": {
    "agent-knowledge": {
      "type": "stdio",
      "command": "/home/alice/.local/bin/wfm-rag",
      "args": ["mcp", "--project", "api", "--project", "frontend"]
    }
  }
}
```

`command` is required. `type` may be omitted, because OMP infers stdio from a command entry. Verify inside a session with `/mcp list` and `/mcp test agent-knowledge`. OMP exposes the tools as `mcp__agent_knowledge_search` and `mcp__agent_knowledge_get`. Give each repository a unique server name, because a same-named definition in a higher-priority source silently replaces the lower one.

### Generic stdio client

```json
{
  "mcpServers": {
    "agent-knowledge": {
      "command": "/home/alice/.local/bin/wfm-rag",
      "args": ["mcp", "--project", "api"]
    }
  }
}
```

Some clients call the map `servers`, or put the executable and its arguments in one array. Adapt only that envelope. The process contract stays: `mcp --project <id>` on stdio, protocol JSON on stdout only, diagnostics on stderr only, and shutdown by closing stdin.

### Tools And Limits

The server exposes exactly two tools:

- `agent_knowledge_search`: required `project` plus `query`, and optional `paths`, `packages`, `types`, `limit`;
- `agent_knowledge_get`: required `project` plus the exact card `id`.

Both inputs are closed-world (`additionalProperties: false`) and constrain `project` to a sorted enum of the IDs allowlisted at launch. No input accepts a repository path, index path, write operation, or shell command.

Registry membership is fixed at launch. `project init` and `project remove` change a running server only after the client restarts it. Rebuilt index content reloads live. A project with a missing or invalid index degrades to a stderr startup warning and an actionable per-call error that names the repair command, and healthy projects keep serving.

Field tables, precedence and name-shadowing rules, tool schemas, degraded-project behavior, and client troubleshooting are in [`docs/mcp-clients.md`](docs/mcp-clients.md).

## CLI Reference

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

Put flags before the positional query or card ID, and quote multiword queries. `--project` is required on `index`, `search`, and `get`, and it is the only locator of a corpus. `--index` moves the index file for those three commands, and the expected root stays the selected project's registered root.

| Command | Behavior |
| --- | --- |
| `index` | Full build and atomic replacement. Reports active cards, skipped cards, chunks, source bytes, warnings, output path, and generation time. A failed build preserves the previous valid index. |
| `search` | Lexical search in one project. `--path`, `--package`, and `--type` are repeatable: values inside one kind are ORed, and different kinds are ANDed. `--limit` defaults to 5 and allows 1 through 20. No matches is a successful result. |
| `get` | Exact, case-sensitive ID lookup. Returns the complete original Markdown with frontmatter, source, and citations. |
| `mcp` | stdio MCP server for the launch allowlist, up to 64 projects, or `--all-projects` for the whole registry. |
| `project init` | Register, refresh, or rebind a project, then scaffold the missing knowledge-base skeleton. `--dir` defaults to the working directory. |
| `project remove` | Delete the registry entry only. |
| `project list` | Registered projects with derived index path and `root missing` / `index missing` status. It does not open index files. |
| `project healthcheck` | Registry liveness, index presence, and dangling index directories. `--verify` also loads each index. `--purge` deletes only safe dangling directories. |

Search lowercases and tokenizes Unicode letters and numbers. It does not stem and it does not expand synonyms, so `parse`, `parser`, and `parsing` are three different terms.

Output and exit status:

- Text is the default. `--format json` writes structured success output to stdout.
- Errors always go to stderr.
- Exit `0` means success, including no search results and a clean `project healthcheck`.
- Exit `2` means invalid command, flag, query, filter, limit, format, ID, or project selection.
- Exit `1` means an operational failure such as a missing or invalid index, a malformed corpus, a registry problem, or a `project healthcheck` that reported findings.

[`docs/cli.md`](docs/cli.md) documents every stable JSON error code and the exact output shape.

## Knowledgebase

Cards are Markdown files under `.agents/knowledge/<category>/`. Only regular files with a lowercase `.md` extension are candidates, and nested directories are allowed. A file named exactly `README.md`, a file directly under `.agents/knowledge/`, a symlink, and an unknown category are all skipped.

The category controls discovery. The frontmatter `type` controls card semantics and filtering. The recommended mapping is:

| Type | Directory | Purpose |
| --- | --- | --- |
| `pattern` | `patterns/` | A recommended workflow or implementation shape |
| `anti-pattern` | `anti-patterns/` | A recurring mistake to avoid, and why |
| `dependency` | `dependencies/` | An external tool or package and its non-obvious behavior |
| `incident` | `incidents/` | Current state of one ongoing incident plus its concise history |
| `research` | `research/` | Current state of one ongoing research target plus its concise history |
| `state` | `state/` | Current project state: live inventories, pending work, open questions |
| `reference`, `decision` | `sources/` | Time-independent facts, external contracts, and architectural decisions |

Every card needs YAML frontmatter and one level-one heading:

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
last_verified: 2026-10-16
---

# INI List Parsing

## Guidance

Read the exact section and key, then use `Key.Strings(",")`.

## Verify

- Read the cited implementation.
- Run the relevant tests.
```

Required fields are `id`, `type`, `status`, and the first `# Heading`. `id` is stable and unique across active, draft, and retired cards inside one project, up to 256 UTF-8 bytes. `type` is one of the eight values in the table above. `status` is `active`, `draft`, or `retired`. Include `scope`, `triggers`, `packages`, `paths`, `source_confidence`, and `last_verified`, or indexing reports an omitted-metadata warning. `paths` are citations and ranking hints, and `wfm-rag` never opens the cited files. Only active cards enter the index, so a `draft` card is invisible to search and get while it still reserves its ID.

Use H2 sections to create focused retrieval chunks, and keep the important guidance after the H1.

### Lifecycle

| Task | How |
| --- | --- |
| Add | Copy a template from `.agents/knowledge/templates/`, write it into an allowed category with a project-unique ID and `status: draft`, then set `active` and reindex. |
| Update | Edit the canonical Markdown, keep the ID unless identity truly changes, update `last_verified` only after you re-check the evidence, then reindex. |
| Retire | Set `status: retired` and reindex. The card leaves search and get, and the ID stays reserved. |
| Delete | Remove the file through the repository's normal version control, then reindex. |
| Validate | `index` is the validator, and there is no validate-only command. Build to a scratch path to check the corpus without touching the live index. |
| List | No command or tool lists cards, and a blank query is invalid. Inspect `documents` in the generated index, or inventory source files with git. |

```bash
wfm-rag index --project api                                            # publish card changes
wfm-rag search --project api "comma separated config"                  # dogfood likely wording
wfm-rag get --project api ini-list-parsing                             # confirm full content

check_dir="$(mktemp -d)"                                               # validate only
wfm-rag index --project api --index "$check_dir/index.json" --format json
rm -rf "$check_dir"

index_path="$(wfm-rag index --project api --format json | jq -r '.index')"
jq -r '.documents[] | [.id, .type, .source] | @tsv' "$index_path"      # active cards only
```

Editing Markdown never changes CLI or MCP results until an `index` run succeeds.

### Agent Skill

`project init` scaffolds `.agents/skills/agent-knowledge/SKILL.md` with the project ID rendered in. The canonical copy-ready copy is `.agents/skills/agent-knowledge/SKILL.md` in this repository. Route knowledge retrieval and card lifecycle tasks to it from the target repository's `AGENTS.md`, and keep repository-specific rules in the skill's final profile section. Restart the agent client after you copy or edit a skill, because clients load skills at startup.

[`docs/knowledge-authoring.md`](docs/knowledge-authoring.md) owns the full schema, placement rules, hygiene sweep, limits, and the add/update/retire/delete workflow.

## Documentation Map

| Document | Owns |
| --- | --- |
| [`docs/cli.md`](docs/cli.md) | Every command, flag, validation rule, output shape, and error code |
| [`docs/knowledge-authoring.md`](docs/knowledge-authoring.md) | Card schema, category rules, and lifecycle workflow |
| [`docs/mcp-clients.md`](docs/mcp-clients.md) | OpenCode, OMP, and generic stdio setup, tool schemas, degraded projects |
| [`docs/architecture.md`](docs/architecture.md) | Package ownership, data flow, and invariants |
| [`docs/index-format.md`](docs/index-format.md) | Persisted JSON contract and atomic replacement |
| [`docs/troubleshooting.md`](docs/troubleshooting.md) | Symptom-based diagnosis and repair commands |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Development workflow and verification |
| [`docs/planning/`](docs/planning/) | Design records for v0.1, the superseded multi-root experiment, and project-scoped retrieval |

## Contributing

Required checks after Go changes:

```bash
gofmt -w cmd/wfm-rag/*.go internal/*/*.go
go test -race -count=1 ./...
go vet ./...
```

`docs/architecture.md` maps the packages, and [`AGENTS.md`](AGENTS.md) plus `.agents/skills/` define the working rules for humans and coding agents. The CLI, index and search core, project registry with healthcheck, read-only stdio MCP server, and integration tests are implemented. This is internal local research software, and it is not intended for public distribution.
