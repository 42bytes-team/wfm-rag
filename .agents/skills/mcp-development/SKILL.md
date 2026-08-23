---
name: mcp-development
description: Use for MCP tools, typed schemas, stdio transport, protocol lifecycle, index reload, cancellation, client compatibility, or MCP integration tests in wfm-rag.
---

# MCP Development

Load `go-backend` and `go-test-writing`. Verify APIs against the exact official SDK version pinned in `go.mod`, not examples from SDK `main`.

## Concrete Surface

- `internal/mcpserver.GetInput`: exact get schema.
- `internal/mcpserver.ErrReload`: changed-index load failure.
- `internal/mcpserver.New`: initial validated index load and typed server construction.
- `internal/mcpserver.Run`: one session over a supplied SDK transport.
- `cmd/wfm-rag.runMCP`: flags, index-path resolution, and real `mcp.StdioTransport` startup.

Default index resolution belongs to `cmd`, not `mcpserver`.

## Fixed V0.1 Contract

Expose exactly:

- `agent_knowledge_search` with shared `search.Query` input and `search.Response` output;
- `agent_knowledge_get` with `GetInput` and shared `search.GetResponse` output.

Both tools are read-only and closed-world. Never accept root, project, index path, write, shell, or indexing inputs. One process serves the root selected at startup.

Do not duplicate search/get behavior or output types in MCP.

## Handler Order

1. SDK validates generated object schema and rejects unknown fields.
2. Handler performs semantic `search.ValidateQuery` or `search.ValidateID`.
3. Check request cancellation.
4. Inspect/reload configured index.
5. Call shared `search.Search` or `search.Get`.
6. Check cancellation after core work.
7. Return typed output; SDK supplies structured content and JSON text fallback.

Validation must precede reload so invalid input remains an input error when the index is broken.

## Reload Contract

The in-memory state is protected by a mutex. Before each valid search/get:

1. Compare file size, modification time, and identity (`os.SameFile`).
2. If unchanged, use current validated value.
3. If changed, stat before load.
4. Call root-bound `index.Load`.
5. Stat after load.
6. Reject if the fingerprint changed during loading.
7. Publish only the stable validated value.

Return `ErrReload` on a changed index that cannot load. Never silently serve stale memory. A later valid replacement must recover without process restart.

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
- protocol-only stdout and stderr isolation;
- request cancellation;
- stdin EOF shutdown;
- startup failures.

Add a direct legacy-first `initialize` subprocess case before changing compatibility behavior or strengthening that compatibility claim.

Do not make fast production search/get artificially block for cancellation tests. Add a test-only blocking tool to a helper subprocess.

## Client Documentation

Update `docs/mcp-clients.md` when executable arguments, tool schemas, transport, compatibility, or lifecycle changes. OpenCode and OMP use different config shapes; verify their current schemas rather than guessing.
