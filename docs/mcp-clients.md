# MCP Client Configuration

`wfm-rag` is a local stdio MCP server. One process serves the projects allowlisted at launch — at most 64 — and exposes only these read-only tools:

- `agent_knowledge_search`
- `agent_knowledge_get`

Every call names exactly one allowlisted project through a required `project` input field. The server reports implementation version `0.3.0`.

It does not start an HTTP server, bind a port, expose a URL, or support HTTP, SSE, or Streamable HTTP transport. A compatible client must launch a local stdio process.

The client starts the process. Do not start `wfm-rag mcp` separately when using one of the configurations below.

## Launch Grammar

```text
wfm-rag mcp --project <id> [--project <id> ...]
wfm-rag mcp --all-projects
```

- `--project` is repeatable and forms the launch allowlist, at most 64 values; more exits 2 with `project_limit_exceeded`.
- `--all-projects` selects the whole registry and cannot be combined with `--project`.
- With neither flag, the command exits 2 with `project_required`.
- `mcp` does not accept `--index`; every served project uses its derived index path.
- `--format text|json` only selects the format of startup errors. Startup failures are written to stderr only, and stdout stays empty.

A `--project` value that is malformed or not in the registry exits 2 at launch (`project_id_invalid` / `project_unknown`, listing the registry IDs) before the server starts. A server that started anyway would advertise a dead enum entry, so the typo surfaces at launch instead of through tool errors.

## Install And Index

From the `wfm-rag` repository root, install a stable binary in a location visible to desktop applications and terminal clients:

```bash
GOBIN="$HOME/.local/bin" go install ./cmd/wfm-rag
test -x "$HOME/.local/bin/wfm-rag"
```

Use absolute paths in client configuration. Register each repository the MCP process will serve, then build its index:

```bash
wfm-rag project init api --dir /home/alice/projects/api
wfm-rag index --project api

wfm-rag project init frontend --dir /home/alice/projects/frontend
wfm-rag index --project frontend
```

Indexes and the project registry live in the state directory: `$XDG_DATA_HOME/wfm-rag` when set and absolute, otherwise `$HOME/.local/share/wfm-rag`. Run the client and the indexing commands as the same OS user, because the state directory is user-owned `0700` data.

Re-run `wfm-rag index --project <id>` after editing knowledge cards. A running MCP process detects the atomic replacement and reloads that project's index before the next valid search or get for it.

## OpenCode

Verified against OpenCode 2.0.26. Add the server to the target repository's `opencode.json` or `opencode.jsonc`, or to the global `~/.config/opencode/opencode.json`. Project configuration is recommended because the allowlist is explicit per client.

V2 places servers under `mcp.servers`, not directly under `mcp`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "api-agent-knowledge": {
        "type": "local",
        "command": [
          "/home/alice/.local/bin/wfm-rag",
          "mcp",
          "--project", "api",
          "--project", "frontend"
        ]
      }
    }
  }
}
```

Or let OpenCode write the entry and then check it:

```bash
cd /home/alice/projects/api
opencode mcp add api-agent-knowledge -- /home/alice/.local/bin/wfm-rag mcp --project api
opencode mcp list
```

`opencode mcp list` prints each configured server with its status (`connected`, `pending`, or `disabled`).

Local server fields:

| Field | Required | Effect |
| --- | --- | --- |
| `type` | yes | must be `"local"` |
| `command` | yes | the executable followed by every argument, in one array |
| `cwd` | no | process directory; a relative path resolves from the workspace |
| `environment` | no | variables added to the inherited process environment; `{env:NAME}` substitutes from the caller. V2 does not read `env` |
| `disabled` | no | `true` keeps the entry configured without connecting it; V2 has no `enabled` field |
| `codemode` | no | `true` by default, which exposes MCP tools through Code Mode. Set `false` to expose `agent_knowledge_search` and `agent_knowledge_get` directly |
| `timeout` | no | per-server timeout overrides in milliseconds |

A higher-precedence project entry with the same name replaces the whole server object instead of merging with the global one, so repeat every field you need in the override, or use distinct names per repository.

Older configurations that put server names directly under `mcp` with `enabled: true` are still read and normalized to `mcp.servers` with `disabled: false`, which is why a V1-era global entry keeps working. New entries should use the V2 shape above.

After changing configuration, quit and restart OpenCode because configuration is loaded at startup. In a prompt, explicitly request the server and name the project when retrieval matters:

```text
Use the agent-knowledge MCP tools to find repository guidance in project api for parsing a comma-separated config value, then retrieve the complete best-matching card.
```

## OMP (Oh My Pi)

Verified against OMP 18.8.9. OMP reads OMP-native MCP files before any imported third-party configuration.

- Project: `.omp/mcp.json`. It is keyed to the working directory, so start OMP from the repository root.
- User: `~/.omp/agent/mcp.json`, or `~/.omp/profiles/<name>/agent/mcp.json` when `omp --profile <name>` is active. A profile sees only its own user-level servers.
- Portable fallback: root `mcp.json` or `.mcp.json`, which other MCP clients can also read.

```json
{
  "$schema": "https://raw.githubusercontent.com/can1357/oh-my-pi/main/packages/coding-agent/src/config/mcp-schema.json",
  "mcpServers": {
    "agent-knowledge": {
      "type": "stdio",
      "command": "/home/alice/.local/bin/wfm-rag",
      "args": [
        "mcp",
        "--project", "api",
        "--project", "frontend"
      ]
    }
  }
}
```

For a stdio entry, `command` is required and `type` may be omitted because discovery infers stdio from a command entry; keep `type: "stdio"` for a schema-valid, portable definition. `args`, `env`, and `cwd` are optional.

Precedence and shadowing:

- Within OMP-native config, project `.omp/mcp.json` precedes the active profile's user file.
- OMP-native config outranks imported third-party config (Claude Code, Agent Plugins, Codex, Gemini CLI, OpenCode, Cursor, Windsurf, VS Code, then root `mcp.json` / `.mcp.json`), and the first definition of a name wins. Duplicate names are not merged.
- A same-named entry in a higher-priority source silently replaces the lower one, so an imported global OpenCode entry can bind OMP to a different allowlist than the project entry you edited. Use unique per-repository names such as `api-agent-knowledge`, or add an OMP-native project entry, which outranks every imported OpenCode entry.
- `disabledServers` in the active profile's user file hides a discovered server by name regardless of its source. `mcp.enableProjectConfig: false` excludes every project-level source, letting a same-named user entry survive.

In-session commands:

```text
/mcp list
/mcp test agent-knowledge
/mcp reconnect agent-knowledge
/mcp reload
```

`/mcp add`, `/mcp enable`, and `/mcp disable` write OMP-managed files and add the `$schema` line automatically; `/mcp reload` rediscovers after those managed changes. After an edit made by another editor, restart OMP when `/mcp list` still reports stale file contents.

OMP exposes each server's tools under an `mcp__`-prefixed normalized name, for example `mcp__agent_knowledge_search`. Use `/mcp list` rather than constructing a runtime name by hand.

## Generic Stdio Client

Many local MCP clients accept the de facto `mcpServers` JSON shape:

```json
{
  "mcpServers": {
    "agent-knowledge": {
      "command": "/home/alice/.local/bin/wfm-rag",
      "args": [
        "mcp",
        "--project",
        "api"
      ]
    }
  }
}
```

Some clients call the map `servers`, require `type: "stdio"`, or place the executable and arguments in one array. Adapt only that client-side envelope. The process contract remains:

- executable: `/absolute/path/to/wfm-rag`
- arguments: `mcp --project <id>` (repeat `--project`, or use `--all-projects`)
- transport: stdio with one JSON-RPC object per line
- stdout: protocol messages only
- stderr: diagnostics only
- shutdown: close the process stdin and wait for exit; stdin EOF exits the server

If the process does not exit within a reasonable client timeout after stdin closes, send `SIGTERM`, wait again, and use `SIGKILL` only as the final fallback.

The repository subprocess suite directly covers the current `server/discover` handshake. The pinned SDK also accepts the earlier `initialize` flow that some clients send first, and both OpenCode 2.0.26 and OMP 18.8.9 connect to this server in the operator's own configuration, but that older first-frame sequence is not yet a dedicated repository subprocess test. A conforming client should negotiate the protocol rather than sending tool requests before initialization completes.

## Tool Schemas

`tools/list` returns exactly two tools, and both inputs are closed-world: `additionalProperties: false`, a required `project` field, and no repository, index-path, write, or shell parameter. The `project` enum is exactly the launch allowlist, sorted, so a restricted server never advertises corpora it cannot serve. Excerpt of `agent_knowledge_search` `inputSchema` for a server launched with `--project api --project frontend`:

```json
{
  "type": "object",
  "properties": {
    "project": {
      "type": "string",
      "description": "configured project ID to search",
      "enum": ["api", "frontend"]
    },
    "query": { "type": "string", "description": "search text" },
    "paths": { "description": "cited path filters" },
    "packages": { "description": "package filters" },
    "types": { "description": "card type filters" },
    "limit": { "description": "result limit, 1 to 20" }
  },
  "required": ["project", "query"],
  "additionalProperties": false
}
```

`agent_knowledge_get` follows the same contract:

```json
{
  "type": "object",
  "properties": {
    "project": {
      "type": "string",
      "description": "configured project ID",
      "enum": ["api", "frontend"]
    },
    "id": { "type": "string", "description": "stable, case-sensitive card ID" }
  },
  "required": ["project", "id"],
  "additionalProperties": false
}
```

A call whose `project` value is outside the enum is rejected by the schema. Defense in depth: a value that reaches the handler anyway returns a tool error naming `project_unknown` and listing only the configured IDs, in sorted order, without touching any index.

## Working Tool Calls

One JSON-RPC object per line on stdin; the matching response on stdout. Search:

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"agent_knowledge_search","arguments":{"project":"api","query":"config parser comma separated"}}}
```

```json
{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"{\"project\":\"api\",\"root\":\"/home/alice/projects/api\",\"indexed_at\":\"2026-10-09T10:14:22Z\",\"query\":\"config parser comma separated\",\"results\":[…]}"}],"structuredContent":{"project":"api","root":"/home/alice/projects/api","indexed_at":"2026-10-09T10:14:22Z","query":"config parser comma separated","results":[{"id":"ini-list-parsing","type":"dependency","title":"INI List Parsing","score":22,"reasons":["trigger: comma-separated","text terms: config, parser"],"excerpt":"Use `Key.Strings(\",\")` for comma separated config.","source":".agents/knowledge/dependencies/ini-list-parsing.md","heading":"Guidance","citations":["src/pkgs/config/ini.go"]}]},"isError":false}}
```

Get:

```json
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"agent_knowledge_get","arguments":{"project":"api","id":"ini-list-parsing"}}}
```

```json
{"jsonrpc":"2.0","id":4,"result":{"content":[{"type":"text","text":"{\"project\":\"api\",…}"}],"structuredContent":{"project":"api","root":"/home/alice/projects/api","indexed_at":"2026-10-09T10:14:22Z","card":{"id":"ini-list-parsing","type":"dependency","status":"active","title":"INI List Parsing","source":".agents/knowledge/dependencies/ini-list-parsing.md","content":"---\n…"}},"isError":false}}
```

Every success carries structured content identical to the CLI `--format json` envelope, plus a JSON text fallback in `content`. Tool failures are MCP tool errors (`isError: true` with a text message), not CLI error envelopes.

## Degraded Projects

An allowlisted project whose index is missing or invalid does not abort startup. The server prints one stderr warning per such project and starts normally:

```text
warning: project "frontend": no valid index at /home/alice/.local/share/wfm-rag/77aa34…/index.json; calls selecting it will fail until `wfm-rag index --project frontend` succeeds: index missing: "/home/alice/.local/share/wfm-rag/77aa34…/index.json"
```

Calls for healthy projects work normally. A call that selects the degraded project re-attempts its load; if it still fails, the tool result is an error written for the model to relay verbatim:

```text
index reload failure: index_missing: project "frontend" has no valid index at "/home/alice/.local/share/wfm-rag/77aa34…/index.json" (index missing: …). Ask the operator to run: wfm-rag index --project frontend. Do not retry with a different project
```

Recovery does not require a client restart: once the operator runs the repair command, the next call for that project loads the new index through the normal fingerprint reload. A broken project never affects calls for other projects.

## Restart Rule

Registry membership is fixed at launch. `project init` and `project remove` affect new CLI invocations immediately, and affect a running MCP process only after the client restarts it; the tool enum, not the live registry, defines what a session can select. `project remove` is not revocation. Index content, by contrast, reloads live: a rebuilt index is picked up by the next valid call for that project without any restart.

## Multiple Projects

Prefer one entry with an explicit `--project` allowlist over one entry per repository:

```json
{
  "mcpServers": {
    "agent-knowledge": {
      "command": "/home/alice/.local/bin/wfm-rag",
      "args": [
        "mcp",
        "--project", "api",
        "--project", "frontend"
      ]
    }
  }
}
```

Every server publishes its own copy of both tool definitions into every model request of an agent session, so four single-project entries cost four tool pairs before the agent reads a single card. One entry keeps one tool pair for every allowlisted project. Measured cost of one `wfm-rag` server in an OMP session is about 207 tokens per request; see [`docs/planning/agent-context-cost-multi-root-mcp.md`](planning/agent-context-cost-multi-root-mcp.md).

Each project must be registered and indexed separately. Retrieval is strictly per project:

- One call reads exactly one project index; cross-project questions are separate calls naming each project.
- Card IDs are unique within one project corpus. The same ID in two projects is two distinct cards, selected by the `project` field.
- The `limit` applies to the selected project's result list.

OMP exposes each configured MCP server's tools under an `mcp__`-prefixed name, so a single `agent-knowledge` entry yields two tool routes regardless of allowlist size.

## Validation

Before debugging a client, verify the same project through the CLI:

```bash
/home/alice/.local/bin/wfm-rag search --project api "config parser comma separated"
/home/alice/.local/bin/wfm-rag get --project api ini-list-parsing
```

Expected client-visible behavior:

- `tools/list` contains exactly `agent_knowledge_search` and `agent_knowledge_get` before any client-added prefix.
- Both inputs require `project`, constrained to the sorted launch-allowlist enum, with `additionalProperties: false`.
- Search responses contain `project`, `root`, `indexed_at`, `query`, and ranked results with score reasons, excerpts, source headings, and citations.
- Get responses contain complete original Markdown, metadata, the project, its root, source, and citations.
- Inputs have no repository, index-path, write, or shell parameter.

## Troubleshooting

### The executable is not found

Use an absolute executable path. GUI applications often have a smaller `PATH` than an interactive shell. Do not configure `go run`; use the installed binary to avoid startup delays and build diagnostics.

### The server refuses to start

Startup failures exit 2 (bad selection) or 1 (registry missing or invalid) and write only to stderr. `project_unknown` at launch means the allowlisted ID is not in the registry: register it with `project init`, fix the typo, or remove it from the client arguments. Inspect the registry with `wfm-rag project list` and `wfm-rag project healthcheck`.

### The index is missing

Run `wfm-rag index --project <id>` as the same OS user that starts the client. A degraded startup warning does not block healthy projects; the degraded one recovers on its next call after the repair.

### The index belongs to another root

Indexes are root-bound. After moving a checkout, run `wfm-rag project init <id> --dir <new path>` to rebind the ID, then `wfm-rag index --project <id>`.

### Card edits are not visible

Markdown changes do not update the index automatically. Re-run `wfm-rag index --project <id>`; the running server reloads before the next valid search or get for that project.

### The client shows no tools

Check the server with the CLI first, then inspect client-specific status:

```bash
opencode mcp list
```

For OMP, run `/mcp list` and `/mcp test agent-knowledge`, and restart OMP if a manually edited MCP file still shows stale contents. Check client logs or captured stderr for startup errors. Any wrapper around `wfm-rag` must keep stdout completely free of banners and diagnostics.

### A tool call cannot select another project or modify files

This is intentional. The launch arguments are the allowlist; extend it with additional `--project` values (or `--all-projects`) and restart the client so the tool enum is rebuilt. Indexing remains an explicit CLI operation and is not exposed as an MCP tool.
