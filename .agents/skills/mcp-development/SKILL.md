---
name: mcp-development
description: Use for MCP tools, typed schemas, stdio transport, protocol lifecycle, index reload, cancellation, client compatibility, or MCP integration tests in wfm-rag.
---

# MCP Development

Load `go-implementation` and `go-test-writing`. Verify APIs against the exact official SDK version pinned in `go.mod`, not examples from SDK `main`.

## Concrete Surface

- `internal/mcpserver.Source`: one launched project — ID, canonical root, index path.
- `internal/mcpserver.SearchInput` / `GetInput`: closed-world envelopes with a required `project`.
- `internal/mcpserver.ErrReload`: a selected project's index is missing, invalid, or changed and could not reload.
- `internal/mcpserver.LoadWarning`: a launched project whose initial load failed; the server still starts degraded.
- `internal/mcpserver.New`: per-source attempted load, typed server construction with the launch-scoped project enum, and warning reporting; `cmd` starts the real `mcp.StdioTransport` and prints warnings to stderr.

Default index resolution belongs to `cmd` and `internal/project`, not `mcpserver`.

## Fixed V0.1 Contract

Expose exactly:

- `agent_knowledge_search` with `SearchInput` and shared `search.Response` output;
- `agent_knowledge_get` with `GetInput` and shared `search.GetResponse` output.

Both tools are read-only and closed-world. `project` is required and its schema carries a sorted enum of the IDs configured at launch; never accept root, index path, write, shell, or indexing inputs. One process serves the projects configured at launch, up to 64, chosen only through `mcp --project` / `mcp --all-projects`.

Do not duplicate search/get behavior or output types in MCP.

Each call reads exactly one project; there is no merged search and no cross-corpus ID resolution. A degraded project's tool error is written for the model to relay: `index_missing`, the project ID, the repair command `wfm-rag index --project <id>`, and the instruction not to retry with a different project.

## Handler Order

1. SDK validates the generated object schema, the `project` enum, and rejects unknown fields.
2. Handler performs semantic `search.ValidateQuery` or `search.ValidateID`.
3. Check request cancellation.
4. Select the named project and inspect/reload only that project's index.
5. Call shared `search.Search` or `search.Get`.
6. Check cancellation after core work.
7. Return typed output; SDK supplies structured content and JSON text fallback.

Validation must precede reload so invalid input remains an input error when the index is broken.

## Reload Contract

The in-memory state holds one validated entry per launched project, protected by a mutex. Before each valid search/get for the selected project:

1. Compare the project's index file size, modification time, and identity (`os.SameFile`).
2. If unchanged and loaded, use the current validated value.
3. If changed or degraded, stat before load.
4. Call root-bound `index.Load`.
5. Stat after load.
6. Reject if the fingerprint changed during loading.
7. Publish only the stable validated value for that project.

Return `ErrReload`-wrapped actionable text when the selected project's index changed and cannot load. Other projects are unaffected; the server never serves a partially refreshed project and never silently serves stale memory. A later valid replacement must recover without process restart.

## Stdio Safety

- Stdout contains only SDK protocol frames.
- Startup/operational diagnostics go to stderr through command error handling.
- Do not print usage, logging, banners, or debug output to stdout in MCP mode.
- Do not wrap the transport with a process that writes stdout diagnostics.
- Honor request cancellation and process-context cancellation.
- Clean stdin EOF ends the server.
- There is no HTTP/SSE/Streamable HTTP transport in v0.1.

## Protocol Compatibility

The current subprocess suite directly exercises `server/discover`. The pinned SDK is also intended to accept earlier `initialize` clients, and installed OMP compatibility has been manually checked, but that older first-frame sequence does not yet have a repository subprocess test. Keep client claims explicit about that evidence. If upgrading the SDK, re-check:

- `mcp.AddTool` generic signatures and schema inference;
- handshake/version negotiation;
- structured-output and text-fallback behavior;
- annotation serialization;
- cancellation and EOF behavior.

## Tests

In-memory tests cover handler semantics. Real subprocess tests cover:

- current SDK `server/discover` behavior;
- exactly two tools and safe input schemas;
- search/get calls and CLI parity;
- structured/text semantic equality;
- running-process atomic reload;
- multi-project launch with per-project isolation, degraded-project startup warnings with recovery without a client restart, and per-project reload failure;
- protocol-only stdout and stderr isolation;
- request cancellation;
- stdin EOF shutdown;
- startup failures.

Add a direct legacy-first `initialize` subprocess case before changing compatibility behavior or strengthening that compatibility claim.

Do not make fast production search/get artificially block for cancellation tests. Add a test-only blocking tool to a helper subprocess.

## Client Documentation

Update `docs/mcp-clients.md` when executable arguments, tool schemas, transport, compatibility, or lifecycle changes, and update the `README.md` "Connect An LLM Through MCP" section in the same change. OpenCode and OMP use different config shapes; verify their current schemas rather than guessing.

Shapes verified against OpenCode 2.0.26 and OMP 18.8.9:

- OpenCode: servers live under `mcp.servers`; `type: "local"` plus a full `command` array are required; `disabled` replaces `enabled`; `environment` (never `env`), `cwd`, `codemode`, and `timeout` are optional; a same-named project entry replaces the whole server object.
- OMP: `.omp/mcp.json` for the project and `~/.omp/agent/mcp.json` for the active profile, both using `mcpServers` with `command`, `args`, and an optional `type` that defaults to stdio; OMP-native config outranks imported OpenCode config.
- Confirm discovery with `opencode mcp list`, `opencode debug config`, and OMP's `/mcp list` or `/mcp test <name>` before stating client behavior.
