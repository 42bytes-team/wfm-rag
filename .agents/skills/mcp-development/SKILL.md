---
name: mcp-development
description: Use for MCP server tools, typed schemas, stdio transport, protocol lifecycle, reload behavior, or MCP integration tests in wfm-rag.
---

# MCP Development

Use the official tagged Go SDK API pinned in `go.mod`. Verify signatures against the installed module or tagged documentation rather than examples from `main`.

## V0.1 Contract

- Expose only `agent_knowledge_search` and `agent_knowledge_get`.
- Use typed input and output structs with generic `mcp.AddTool`.
- Do not accept project or root paths in tool inputs.
- Do not expose write, add, update, delete, indexing, or shell tools.
- Use the same search/get core as the CLI.
- Reload the JSON index when its modification time changes before a request.
- Return reload errors instead of serving a stale snapshot.

## Stdio Safety

- Stdout is protocol-only.
- Send diagnostics and process errors to stderr.
- Do not let flag parsing, usage text, logging, or panic output contaminate stdout in MCP mode.
- Honor request cancellation and terminate cleanly on stdin EOF.

## Responses

- Return structured outputs with stable JSON field names.
- Preserve the SDK-compatible text fallback.
- Include index generation time, source path, heading, citations, score, and score reasons where applicable.
- Validate query length, limits, enum filters, and card IDs before executing core behavior.

## Tests

Use in-memory SDK transports for handler behavior and a subprocess test for real stdio framing. The subprocess test must verify initialize, tools/list, tool calls, protocol-only stdout, stderr isolation, cancellation, EOF shutdown, and index reload.
