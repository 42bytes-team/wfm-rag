# CLI Reference

## Usage

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

Top-level help:

```bash
wfm-rag help
wfm-rag -h
wfm-rag --help
```

There is no `version`, `validate`, `add`, `update`, `retire`, or `delete` command. Knowledge-card maintenance is ordinary repository editing followed by `index`.

Every corpus is selected by a registered project ID. `--project` is the sole locator of a repository; there is no path flag, no inference from the working directory, and the retired repeatable root flag is rejected as an undefined flag.

On `index`, `search`, and `get`, flags must appear before the positional query or card ID; the standard Go flag parser stops parsing options at the first positional argument. The `project` subcommands accept the documented positional-first order (`project init <id> [--dir <path>]`) as well as flag-first order.

## Common Flags

`index`, `search`, and `get` accept:

| Flag | Required | Default | Meaning |
| --- | --- | --- | --- |
| `--project <id>` | yes | none | Registered project ID; resolved through the registry to its canonical repository root. Accepted exactly once |
| `--index <file>` | no | root-derived path | Index file override. Accepted exactly once. A relative path is made absolute from the process working directory and cleaned |
| `--format text\|json` | no | `text` | Success and recognized error output format |

`--project` and `--index` are single-use flags: passing either twice exits 2 with `invalid_arguments` (`--project may be given once`).

Project IDs must match `^[a-z0-9][a-z0-9-]{0,31}$`. A malformed ID and an unregistered ID are distinct rejections; see the error table.

## State Directory

The shared state directory is `$XDG_DATA_HOME/wfm-rag` when `$XDG_DATA_HOME` is set and absolute; a relative or empty value is invalid per the XDG Base Directory spec and falls back to `$HOME/.local/share/wfm-rag`. An empty `HOME` is an error. The directory is created on demand with mode `0700` and holds:

```text
<state-dir>/projects.json                      registry, mode 0600
<state-dir>/projects.lock                      flock writer lock, mode 0600
<state-dir>/<root-sha256>/index.json           derived per-project index
```

`root-sha256` is the full lowercase SHA-256 digest of the canonical absolute repository root. Root symlink aliases share an index; moving the real checkout changes the path, and `project init` in the new directory rebinds the project ID.

## `--index` Overrides The File Location Only

Every index file records the canonical root it was built from, and loading refuses any file whose recorded root differs from the selected project's registered root. `--index` changes only which file a command reads or writes:

```bash
wfm-rag index  --project api --index /tmp/ci/index.json   # builds from api's registered root
wfm-rag search --project api --index /tmp/ci/index.json "query"   # reads that file, still root-bound
```

`--index` never substitutes a different checkout, is not stored in the registry, and is not accepted by `mcp`. Use one identical absolute custom path across `index`, `search`, and `get`.

## `index`

```bash
wfm-rag index --project api

wfm-rag index \
  --project api \
  --index /absolute/index.json \
  --format json
```

Behavior:

1. Resolve `--project` through the registry and canonicalize the root.
2. Discover every candidate card.
3. Parse and validate active, draft, and retired cards.
4. Reject duplicate IDs across every status.
5. Skip valid inactive cards with warnings.
6. Build and validate the complete active index.
7. Atomically replace the destination.

Text output reports active-card count, skipped count, chunks, source bytes, index path, generation time, and warnings:

```text
Indexed 1 cards and 2 chunks
Skipped: 1
Source bytes: 470
Index: /home/alice/.local/share/wfm-rag/3f2a8c…/index.json
Generated: 2026-10-09T10:14:22.451233Z
Warning: .agents/knowledge/patterns/draft.md (draft): skipped card with status draft
```

The warning line is `Warning: <source> (<card-id>): <message>`; the card ID part is omitted when the warning has no card.

JSON shape:

```json
{
  "root": "/home/alice/projects/api",
  "indexed_at": "2026-10-09T10:14:22.451233Z",
  "cards": 1,
  "skipped": 1,
  "chunks": 2,
  "source_bytes": 470,
  "warnings": [
    {
      "source": ".agents/knowledge/patterns/draft.md",
      "card_id": "draft",
      "message": "skipped card with status draft"
    }
  ],
  "index": "/home/alice/.local/share/wfm-rag/3f2a8c…/index.json"
}
```

When no warnings exist, `warnings` may be `null` rather than `[]`.

A failed corpus build or pre-rename write leaves the prior valid index untouched.

## `search`

```bash
wfm-rag search --project api "config parser comma separated"

wfm-rag search \
  --project api \
  --path src/pkgs/config/ini.go \
  --path src/apps/scanlan-discord/configs.go \
  --package gopkg.in/ini.v1 \
  --type dependency \
  --limit 10 \
  --format json \
  "config parser"
```

Search-specific flags:

| Flag | Repeatable | Default | Rule |
| --- | --- | --- | --- |
| `--path <path>` | yes | none | Citation-path filter |
| `--package <pkg>` | yes | none | Package filter |
| `--type <type>` | yes | none | Exact card-type filter |
| `--limit <n>` | no | 5 | Explicit range 1 through 20 |

Do not comma-join repeated values; pass the flag again.

Search reads exactly one project index. The limit applies to that project's result list.

Validation:

- query: non-whitespace valid UTF-8, maximum 4,096 bytes;
- each filter kind: maximum 20 values;
- each filter value: trimmed, non-empty valid UTF-8, maximum 1,024 bytes;
- type: one of the eight card types.

Filter composition:

- OR within paths;
- OR within packages;
- OR within types;
- AND across path/package/type kinds.

No matches exits successfully. Text mode prints the project header and `No results.`

JSON response:

```json
{
  "project": "api",
  "root": "/home/alice/projects/api",
  "indexed_at": "2026-10-09T10:14:22Z",
  "query": "config parser comma separated",
  "results": [
    {
      "id": "ini-list-parsing",
      "type": "dependency",
      "title": "INI List Parsing",
      "score": 22,
      "reasons": ["trigger: comma-separated", "trigger: config", "scope: config", "text terms: config, parser"],
      "excerpt": "## Examples\n\n`config.Rabbitmq.Topics` and Scanlan's role and channel lists use the dependency's list parser.",
      "source": ".agents/knowledge/dependencies/ini-list-parsing.md",
      "heading": "Examples",
      "citations": ["src/pkgs/config/ini.go", "src/apps/scanlan-discord/configs.go"]
    }
  ]
}
```

Only one best chunk per card is returned. See `docs/architecture.md` for ranking and tie-breaking.

Text mode prints a `Project:` line and one `Indexed: <timestamp> (<root>)` header line, then one block per result separated by blank lines:

```text
Project: api
Indexed: 2026-10-09T10:14:22Z (/home/alice/projects/api)
1. INI List Parsing [ini-list-parsing] score=22
Type: dependency
Source: .agents/knowledge/dependencies/ini-list-parsing.md#Examples
Citations: src/pkgs/config/ini.go, src/apps/scanlan-discord/configs.go
Reasons: trigger: comma-separated; trigger: config; scope: config; text terms: config, parser
## Examples

`config.Rabbitmq.Topics` and Scanlan's role and channel lists use the dependency's list parser.
```

## `get`

```bash
wfm-rag get --project api ini-list-parsing
wfm-rag get --project api --format json ini-list-parsing
```

The ID is exact and case-sensitive. It must be non-empty, trimmed, valid UTF-8, and at most 256 bytes.

Text output begins with source, citations, and index timestamp, followed by the complete original Markdown including frontmatter:

```text
Source: .agents/knowledge/dependencies/ini-list-parsing.md
Citations: src/pkgs/config/ini.go
Indexed: 2026-10-09T10:14:22Z

---
id: ini-list-parsing
...
```

JSON shape:

```json
{
  "project": "api",
  "root": "/home/alice/projects/api",
  "indexed_at": "2026-10-09T10:14:22Z",
  "card": {
    "id": "ini-list-parsing",
    "type": "dependency",
    "status": "active",
    "scopes": ["go", "config"],
    "triggers": ["INI", "comma-separated"],
    "packages": ["gopkg.in/ini.v1"],
    "paths": ["src/pkgs/config/ini.go"],
    "source_confidence": "high",
    "last_verified": "2026-08-23",
    "title": "INI List Parsing",
    "source": ".agents/knowledge/dependencies/ini-list-parsing.md",
    "content": "---\n..."
  }
}
```

YAML uses `scope`; JSON output uses `scopes`.

## `mcp`

```bash
wfm-rag mcp --project api
wfm-rag mcp --project api --project cloudflare
wfm-rag mcp --all-projects
```

Selection rules:

- `--project` is repeatable and is the launch allowlist, at most 64 values; more exits 2 with `project_limit_exceeded`.
- `--all-projects` selects the whole registry and cannot be combined with `--project`; with an empty registry it exits 2 with `project_required`.
- With neither, the command exits 2 with `project_required`.
- A `--project` value that is malformed or not in the registry exits 2 before the server starts (`project_id_invalid` / `project_unknown` listing the registry IDs).
- `mcp` does not accept `--index`; every served project uses its derived index path.
- `--format` only selects the format of startup errors written to stderr.

A configured project whose index is missing or invalid does not abort startup: the server prints one stderr warning per such project and keeps serving the healthy ones; see `docs/mcp-clients.md`.

Registry membership is fixed at launch. `project init` and `project remove` affect a running server only after the client restarts it. Index content reloads live: a rebuilt index is picked up by the next valid call.

This command starts a stdio MCP server, not an HTTP listener. It normally appears to wait because it reads protocol frames from stdin. An MCP client should launch the process. Stdout is reserved for protocol output and diagnostics go to stderr.

See `docs/mcp-clients.md`.

## `project`

### `project init`

```bash
cd /home/alice/projects/api
wfm-rag project init api

wfm-rag project init ci-api --dir /build/checkout
wfm-rag project init api --dir /new/location --format json
```

Idempotent registration of `id -> canonical root` of `--dir` or the working directory:

- new ID: `Registered api -> /home/alice/projects/api`
- same directory again: `Refreshed api -> /home/alice/projects/api`
- different directory: `Rebound api: /home/alice/projects/api -> /new/location` (rebinding is never implicit; only `init` performs it)

After registration, `init` scaffolds the knowledge-base skeleton in the target
repository, creating only what is missing: the seven category directories,
`.agents/knowledge/README.md`, `.agents/knowledge/schema.md`, eight card
templates under `.agents/knowledge/templates/`, and a starter
`.agents/skills/agent-knowledge/SKILL.md` with the project ID rendered into
every command. Existing files are never overwritten or written through
symlinks, so re-running `init` on a complete KB adds nothing, and re-running it
after a tool upgrade adds only the skeleton files the repository still lacks.
Text output lists what was created, and when the freshly scaffolded starter
skill still carries the profile placeholders it ends with the one manual step:

```text
Scaffolded 11 knowledge-base files:
  .agents/knowledge/README.md
  .agents/knowledge/schema.md
  .agents/knowledge/templates/anti-pattern.md
  ...
  .agents/skills/agent-knowledge/SKILL.md
Next: fill the Repository Profile section of .agents/skills/agent-knowledge/SKILL.md with this repository's real values (prompt: .agents/knowledge/README.md), then run wfm-rag index --project api
```

The scaffolded knowledge README is documentation, not a card: files directly
under `.agents/knowledge/` and any file named `README.md` are never indexed. Its
"First Task For The Agent" block is a copy-paste prompt for the repository's
agent to fill the starter skill's `Repository Profile` with real triggers and
verification commands. The `Next:` line appears only alongside a scaffolded-file
list and only while the starter skill still carries the profile placeholders: a
no-op refresh or a rebind into an already complete target prints nothing extra,
and a filled profile gets no hint. The prompt path is named only when the
knowledge README actually carries the scaffolded prompt, so a repository with a
customized README gets the hint without a path. JSON output carries no hint
field; the created paths are in `scaffolded`.

JSON shape (the `scaffolded` array lists created repository-relative paths and is
omitted when nothing was created):

```json
{
  "project": "api",
  "root": "/new/location",
  "previous_root": "/home/alice/projects/api",
  "scaffolded": [".agents/knowledge/schema.md", "..."],
  "rebound": true,
  "created": false
}
```

`previous_root` appears only when `rebound` is true. A rebind after a moved checkout changes the derived index path; re-run `index` afterward. A skeleton path that exists as the wrong kind of entry fails with `project_scaffold_failed` after the registration already succeeded.

### `project remove`

```bash
wfm-rag project remove api
```

Text: `Removed api (/home/alice/projects/api)`. JSON:

```json
{
  "project": "api",
  "root": "/home/alice/projects/api",
  "rebound": false,
  "created": false
}
```

`remove` deletes only the registry entry. It never touches repository or index files, and it is not revocation for a running MCP process — restart the client.

### `project list`

```bash
wfm-rag project list
wfm-rag project list --format json
```

A cheap registry view, sorted by ID: ID, canonical root, derived index path, and bracketed `[root missing]` / `[index missing]` status. It never opens index files. An empty registry prints `No projects registered.`

```text
api /home/alice/projects/api /home/alice/.local/share/wfm-rag/3f2a8c…/index.json
ghost /home/alice/old/ghost /home/alice/.local/share/wfm-rag/9b1d0e…/index.json [root missing] [index missing]
```

JSON shape:

```json
{
  "projects": [
    {
      "project": "api",
      "root": "/home/alice/projects/api",
      "index_path": "/home/alice/.local/share/wfm-rag/3f2a8c…/index.json",
      "root_missing": false,
      "index_missing": false
    }
  ]
}
```

### `project healthcheck`

```bash
wfm-rag project healthcheck
wfm-rag project healthcheck --verify
wfm-rag project healthcheck --purge --format json
```

A non-interactive sweep of the registry and the whole state directory; it writes nothing except under `--purge`, and never writes into target repositories. Three sweeps: registry liveness, index presence (`--verify` additionally runs the full root-bound index load per project), and dangling index directories under the state directory. `--purge` deletes a dangling directory only when it contains exactly one regular `index.json`; anything else is reported with a `not purged:` detail and preserved.

Findings are printed as data on stdout, and a non-empty finding set then exits 1 with the `healthcheck_findings` error on stderr (`healthcheck reported N finding(s)`), so scripts branch on the exit code. Text mode prints one line per finding (`kind [project] [path] [detail]`), then `Purged: <directory>` lines, or `No problems found.` when the state is clean and nothing was purged:

```text
index_dangling /home/alice/.local/share/wfm-rag/9b1d0e… index directory is not claimed by any registered project
Purged: /home/alice/.local/share/wfm-rag/9b1d0e…
```

JSON shape:

```json
{
  "findings": [
    {
      "kind": "index_missing",
      "project": "web",
      "path": "/home/alice/.local/share/wfm-rag/77aa34…/index.json",
      "detail": "no regular index file at the derived path"
    }
  ],
  "purged": []
}
```

Finding kinds: `root_missing`, `index_missing`, `index_invalid` (only with `--verify`, or when a stored root cannot derive a path), `index_dangling`, `state_unexpected`, and `project_registry_missing` / `project_registry_invalid` when the registry itself fails to load. Findings are sorted by kind, project, path.

Exit 0 when there are no findings; exit 1 when any finding exists, with the findings already written on stdout. Repair paths: `project remove`, re-`init`, `index`, or `--purge`.

## Output Streams

- Successful CLI output: stdout.
- CLI errors: stderr.
- MCP protocol: stdout only.
- MCP startup/process diagnostics: stderr only.

## Exit Codes

| Code | Meaning |
| ---: | --- |
| 0 | Success, including an empty search result and a clean `project healthcheck` |
| 1 | Operational failure, including a `project healthcheck` that reported findings |
| 2 | Invalid command, arguments, flags, query, filters, limit, format, or ID; invalid project selection |

Exactly these codes map to exit 2: `invalid_arguments`, `project_required`, `project_unknown`, `project_id_invalid`, `project_limit_exceeded`. Every other error code exits 1.

## JSON Errors

When `--format json` was recognized, errors use compact one-line JSON on stderr:

```json
{"error":{"code":"index_missing","message":"project \"api\" has no valid index at \"/home/alice/.local/share/wfm-rag/3f2a8c…/index.json\": index missing: \"/home/alice/.local/share/wfm-rag/3f2a8c…/index.json\". Repair: wfm-rag index --project api"}}
```

Text errors begin with `error:`; argument-level text errors also print the usage block.

Stable codes:

| Code | Exit | Meaning |
| --- | ---: | --- |
| `invalid_arguments` | 2 | Command, flags, positionals, format, query, filter, limit, or ID is invalid; a retired or unknown flag; a single-use flag repeated |
| `project_required` | 2 | `--project` absent on `index`, `search`, or `get`; `mcp` given neither `--project` nor `--all-projects`; `--all-projects` with an empty registry |
| `project_unknown` | 2 | Well-formed ID not in the registry, or not selectable at `mcp` launch; the message lists the registered IDs |
| `project_id_invalid` | 2 | ID does not match `^[a-z0-9][a-z0-9-]{0,31}$` |
| `project_limit_exceeded` | 2 | More than 64 `--project` flags on `mcp`; `project init` when the registry already holds 64 projects |
| `project_registry_missing` | 1 | No registry file in the state directory |
| `project_registry_invalid` | 1 | Registry is malformed, has trailing data or duplicate keys, unknown fields, a non-absolute stored root, exceeds 1 MiB, or is a symlink or non-regular file |
| `project_locked` | 1 | Another writer holds `projects.lock`; the lock is never waited on |
| `project_root_missing` | 1 | The registered root is no longer an existing directory; the message names the repair: `wfm-rag project init <id> --dir <new path>` |
| `project_scaffold_failed` | 1 | `project init` registered the project but the KB skeleton could not be completed (a skeleton path exists as the wrong kind of entry); the registration stands and no existing file was modified |
| `index_missing` | 1 | Every retrieval-path index load failure collapses here: missing file, unsupported version, root mismatch, or malformed structure. The message names the project and the repair: `wfm-rag index --project <id>` |
| `invalid_index` | 1 | An index snapshot rejected by build-time validation, such as an oversized generated file |
| `duplicate_card_id` | 1 | Two source cards use one ID, including inactive cards |
| `malformed_card_corpus` | 1 | Frontmatter, required metadata, enum, ID, title, or YAML is invalid |
| `card_not_found` | 1 | Exact active indexed card ID does not exist |
| `healthcheck_findings` | 1 | `project healthcheck` reported findings; the findings themselves are data on stdout |
| `operation_failed` | 1 | Other filesystem, build, or write failure |
