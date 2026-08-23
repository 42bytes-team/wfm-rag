# CLI Reference

## Usage

```text
wfm-rag index --root <repo> [--index <file>] [--format text|json]
wfm-rag search --root <repo> [--index <file>] [--path <path>] [--package <pkg>] [--type <type>] [--limit 5] [--format text|json] <query>
wfm-rag get --root <repo> [--index <file>] [--format text|json] <card-id>
wfm-rag mcp --root <repo> [--index <file>]
```

Top-level help:

```bash
wfm-rag help
wfm-rag -h
wfm-rag --help
```

There are no `version`, `list`, `validate`, `add`, `update`, `retire`, or `delete` commands.

Flags must appear before the positional query or card ID. The standard Go flag parser stops parsing options at the first positional argument.

## Common Flags

`index`, `search`, and `get` accept:

| Flag | Required | Default | Meaning |
| --- | --- | --- | --- |
| `--root <repo>` | yes | none | Repository root, canonicalized through symlinks |
| `--index <file>` | no | root-derived path | Explicit index file; a relative path is made absolute from the process working directory |
| `--format text\|json` | no | `text` | Success and recognized error output format |

`mcp` accepts only `--root` and `--index`. `--format` is invalid in MCP mode.

## `index`

```bash
wfm-rag index --root /absolute/repository

wfm-rag index \
  --root /absolute/repository \
  --index /absolute/index.json \
  --format json
```

Behavior:

1. Canonicalize and validate the root.
2. Discover every candidate card.
3. Parse and validate active, draft, and retired cards.
4. Reject duplicate IDs across every status.
5. Skip valid inactive cards with warnings.
6. Build and validate the complete active index.
7. Atomically replace the destination.

Text output includes active-card count, skipped count, chunks, source bytes, index path, generation time, and warnings.

JSON shape:

```json
{
  "root": "/canonical/root",
  "indexed_at": "2026-08-23T12:00:00Z",
  "cards": 3,
  "skipped": 1,
  "chunks": 18,
  "source_bytes": 4702,
  "warnings": [],
  "index": "/absolute/index.json"
}
```

When no warnings exist, `warnings` may be `null` rather than `[]`.

A failed corpus build or pre-rename write leaves the prior valid index untouched.

## `search`

```bash
wfm-rag search --root /absolute/repository "config parser comma separated"

wfm-rag search \
  --root /absolute/repository \
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

Validation:

- query: non-whitespace valid UTF-8, maximum 4,096 bytes;
- each filter kind: maximum 20 values;
- each filter value: trimmed, non-empty valid UTF-8, maximum 1,024 bytes;
- type: one of the six card types.

Filter composition:

- OR within paths;
- OR within packages;
- OR within types;
- AND across path/package/type kinds.

No matches exits successfully. Text mode prints the index timestamp and `No results.`

An unfiltered JSON response has this shape:

```json
{
  "root": "/canonical/root",
  "indexed_at": "2026-08-23T12:00:00Z",
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

## `get`

```bash
wfm-rag get --root /absolute/repository ini-list-parsing
wfm-rag get --root /absolute/repository --format json ini-list-parsing
```

The ID is exact and case-sensitive. It must be non-empty, trimmed, valid UTF-8, and at most 256 bytes.

Text output begins with source, citations, and index timestamp, followed by the complete original Markdown including frontmatter.

JSON shape:

```json
{
  "root": "/canonical/root",
  "indexed_at": "2026-08-23T12:00:00Z",
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
wfm-rag mcp --root /absolute/repository
wfm-rag mcp --root /absolute/repository --index /absolute/index.json
```

The index must already exist and validate for the supplied root. This command starts a stdio MCP server, not an HTTP listener. It normally appears to wait because it reads protocol frames from stdin.

An MCP client should launch the process. Stdout is reserved for protocol output and diagnostics go to stderr.

See `docs/mcp-clients.md`.

## Default Index Path

```text
~/.local/share/wfm-rag/<root-sha256>/index.json
```

The hash uses the canonical absolute root. Root symlink aliases share an index; moving the real checkout changes it.

## Output Streams

- Successful CLI output: stdout.
- CLI errors: stderr.
- MCP protocol: stdout only.
- MCP startup/process diagnostics: stderr only.

## Exit Codes

| Code | Meaning |
| ---: | --- |
| 0 | Success, including an empty search result |
| 1 | Operational failure |
| 2 | Invalid command, arguments, flags, query, filters, limit, format, or ID |

## JSON Errors

When `--format json` was recognized, errors use compact one-line JSON on stderr:

```json
{"error":{"code":"index_missing","message":"index missing: \"/path/index.json\""}}
```

Stable codes:

| Code | Meaning |
| --- | --- |
| `invalid_arguments` | Command, flags, positionals, format, query, filter, limit, or ID is invalid |
| `index_missing` | Index does not exist |
| `unsupported_index` | Stored version is unsupported |
| `index_root_mismatch` | Index belongs to another canonical root |
| `invalid_index` | Index is malformed, oversized, unsafe, or structurally inconsistent |
| `duplicate_card_id` | Two source cards use one ID, including inactive cards |
| `malformed_card_corpus` | Frontmatter, required metadata, enum, ID, title, or YAML is invalid |
| `card_not_found` | Exact active indexed card ID does not exist |
| `operation_failed` | Other root, filesystem, build, or write failure |

Text errors begin with `error:`. Invalid text-mode arguments also print the usage block.
