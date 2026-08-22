# wfm-rag

`wfm-rag` is an internal local research tool for indexing curated Markdown knowledge cards and exposing deterministic retrieval through a CLI and stdio MCP server.

The CLI and read-only stdio MCP retrieval workflows are implemented. The implementation contract is documented in [`docs/planning/agent-rag-mcp-implementation.md`](docs/planning/agent-rag-mcp-implementation.md).

## Commands

```bash
wfm-rag index --root /path/to/repository
wfm-rag search --root /path/to/repository "query"
wfm-rag get --root /path/to/repository card-id
wfm-rag mcp --root /path/to/repository
```

The MCP server exposes only `agent_knowledge_search` and `agent_knowledge_get`. One process serves the repository root selected at startup, and tool inputs cannot select another root or modify files.

See [MCP client configuration](docs/mcp-clients.md) for OpenCode, OMP (Oh My Pi), and generic stdio client examples.

Markdown under each repository's `.agents/knowledge/` directory remains canonical. The generated JSON index is disposable derived state.

This repository is internal, local-only research software and is not intended for publication.
