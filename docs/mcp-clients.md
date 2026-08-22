# MCP Client Configuration

`wfm-rag` is a local stdio MCP server. One server process serves one fixed repository root and exposes only these read-only tools:

- `agent_knowledge_search`
- `agent_knowledge_get`

The client starts the process. Do not start `wfm-rag mcp` separately when using one of the configurations below.

## Install And Index

From the `wfm-rag` repository root, install a stable binary in a location visible to desktop applications and terminal clients:

```bash
GOBIN="$HOME/.local/bin" go install ./cmd/wfm-rag
test -x "$HOME/.local/bin/wfm-rag"
```

Use absolute paths in client configuration. First build the index for the repository the MCP process will serve:

```bash
/home/alice/.local/bin/wfm-rag index --root /home/alice/projects/api
```

The default index path is derived from the canonical repository root and the current user's home directory. Run the client and indexing command as the same OS user. If an explicit index is preferable, use the identical absolute path in both commands:

```bash
/home/alice/.local/bin/wfm-rag index \
  --root /home/alice/projects/api \
  --index /home/alice/.local/share/wfm-rag/api-index.json
```

Then append these arguments to the client examples:

```text
--index /home/alice/.local/share/wfm-rag/api-index.json
```

Re-run `wfm-rag index` after editing knowledge cards. A running MCP process detects the atomic replacement and reloads it before the next valid search or get operation.

## OpenCode

Add the server to the target repository's `opencode.json` or to the global `~/.config/opencode/opencode.json`. Project configuration is recommended because the server is intentionally bound to one repository.

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "agent-knowledge": {
      "type": "local",
      "command": [
        "/home/alice/.local/bin/wfm-rag",
        "mcp",
        "--root",
        "/home/alice/projects/api"
      ],
      "enabled": true
    }
  }
}
```

OpenCode's `command` field is one array containing the executable and every argument. The current schema uses `environment`, not `env`, when environment variables are needed.

After changing configuration, quit and restart OpenCode because configuration is loaded at startup. Check the connection with:

```bash
opencode mcp list
```

In a prompt, explicitly request the server when retrieval matters:

```text
Use the agent-knowledge MCP tools to find repository guidance for parsing a comma-separated config value, then retrieve the complete best-matching card.
```

OpenCode prefixes MCP tool names with the configured server name in its tool registry. Restrict or enable those tools per agent with the `agent-knowledge_*` tool glob if needed.

## OMP (Oh My Pi)

OMP can use its native project file `.omp/mcp.json`. Start OMP from the repository root, or select it explicitly with `omp --cwd /home/alice/projects/api`, because OMP 17.1.5 does not search ancestor directories for MCP configuration.

```json
{
  "$schema": "https://raw.githubusercontent.com/can1357/oh-my-pi/v17.1.5/packages/coding-agent/src/config/mcp-schema.json",
  "mcpServers": {
    "agent-knowledge": {
      "type": "stdio",
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

For a user-level definition, put the same content in `~/.omp/agent/mcp.json`. Named OMP profiles use `~/.omp/profiles/<name>/agent/mcp.json` instead.

OMP also imports project and global OpenCode MCP configuration. If the OpenCode configuration above applies to the same repository, OMP can use it directly when OMP is launched from that repository root. Prefer either the imported OpenCode entry or an OMP-native entry; do not maintain duplicate definitions unless an OMP-specific override is required.

OMP encounters global OpenCode configuration before project OpenCode configuration. If both scopes use the same server name, the global definition wins and may bind OMP to the wrong repository. Use a unique per-repository name such as `api-agent-knowledge`, or add an OMP-native project definition, which has higher discovery priority than imported OpenCode entries.

OMP 17.1.5 can retain cached file contents across `/mcp reload` after an external editor changes or creates an MCP file. Quit and restart OMP after manual edits. Once restarted, use these commands inside the session:

```text
/mcp list
/mcp test agent-knowledge
```

`/mcp reload` is reliable after OMP-managed changes such as `/mcp add`, enable, or disable. Newer OMP releases may also invalidate externally edited files during reload.

OMP exposes MCP tools with an `mcp__`-prefixed, normalized runtime name. Use `/mcp list` rather than depending on a hand-constructed runtime name.

## Generic Stdio Client

Many local MCP clients accept the de facto `mcpServers` JSON shape:

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

Some clients call the map `servers`, require `type: "stdio"`, or place the executable and arguments in one array. Adapt only that client-side envelope. The process contract remains:

- executable: `/absolute/path/to/wfm-rag`
- arguments: `mcp --root /absolute/path/to/repository`
- transport: stdio with one JSON-RPC object per line
- stdout: protocol messages only
- stderr: diagnostics only
- shutdown: close the process stdin and wait for exit

If the process does not exit within a reasonable client timeout after stdin closes, send `SIGTERM`, wait again, and use `SIGKILL` only as the final fallback.

The server supports the current `server/discover` handshake and the earlier `initialize` flow used by clients such as OMP. A conforming client should negotiate the protocol rather than sending tool requests before initialization completes.

## Multiple Repositories

Tool inputs cannot select a root. Configure one named process per repository:

```json
{
  "mcpServers": {
    "api-knowledge": {
      "command": "/home/alice/.local/bin/wfm-rag",
      "args": ["mcp", "--root", "/home/alice/projects/api"]
    },
    "frontend-knowledge": {
      "command": "/home/alice/.local/bin/wfm-rag",
      "args": ["mcp", "--root", "/home/alice/projects/frontend"]
    }
  }
}
```

Each repository must be indexed separately.

## Validation

Before debugging a client, verify the same index through the CLI:

```bash
/home/alice/.local/bin/wfm-rag search --root /home/alice/projects/api "config parser comma separated"
/home/alice/.local/bin/wfm-rag get --root /home/alice/projects/api ini-list-parsing
```

Expected client-visible behavior:

- `tools/list` contains exactly `agent_knowledge_search` and `agent_knowledge_get` before any client-added prefix.
- Search responses contain `root`, `indexed_at`, ranked results, score reasons, excerpts, source headings, and citations.
- Get responses contain complete original Markdown, metadata, source, and citations.
- Inputs have no `root`, `project`, write, index, or shell parameter.

## Troubleshooting

### The executable is not found

Use an absolute executable path. GUI applications often have a smaller `PATH` than an interactive shell. Do not configure `go run`; use the installed binary to avoid startup delays and build diagnostics.

### The index is missing

Run `wfm-rag index --root <same-absolute-root>` as the same OS user that starts the client. If the MCP configuration includes `--index`, the indexing command must include the same path.

### The index belongs to another root

Indexes are root-bound. Rebuild after moving a checkout, and ensure the configured root and indexing root identify the same repository.

### Card edits are not visible

Markdown changes do not update the index automatically. Re-run `wfm-rag index`; the running server reloads before the next valid search or get operation.

### The client shows no tools

Check the server with the CLI first, then inspect client-specific status:

```bash
opencode mcp list
```

For OMP 17.1.5, restart after manually editing MCP files, then run `/mcp list` and `/mcp test agent-knowledge`. Check client logs or captured stderr for startup errors. Any wrapper around `wfm-rag` must keep stdout completely free of banners and diagnostics.

### A tool call cannot select another project or modify files

This is intentional. Start another named MCP server process for another root. Indexing remains an explicit CLI operation and is not exposed as an MCP tool.
