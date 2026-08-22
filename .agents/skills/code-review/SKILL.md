---
name: code-review
description: Use for code, diff, pull request, or implementation-plan reviews in wfm-rag, with findings focused on correctness, containment, determinism, and MCP protocol safety.
---

# Code Review

Lead with prioritized findings and file/line references. Focus on observable defects and missing tests rather than style preferences.

## Review Checks

- Filesystem reads stay inside the fixed `.agents/knowledge` corpus.
- Symlinks and non-regular files cannot escape containment.
- Card IDs and output ordering are deterministic.
- Index replacement is atomic and a failed build preserves the last valid index.
- Search scoring matches the documented formula and CLI/MCP results are identical.
- Tool schemas reject roots, writes, oversized inputs, and invalid limits.
- MCP stdout cannot be contaminated by logging or CLI output.
- Index reload errors are surfaced instead of serving stale results.
- Tests cover malformed input, boundaries, cancellation, and protocol lifecycle.

If no findings exist, state that and identify residual testing risks.
