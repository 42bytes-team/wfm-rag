---
name: code-review
description: Use for code, diff, pull request, implementation-plan, or documentation reviews in wfm-rag, focused on correctness, containment, determinism, parity, and MCP protocol safety.
---

# Code Review

Lead with prioritized concrete findings and file/line references. Focus on behavioral defects, security/containment, regressions, stale contracts, and missing tests before style.

## Trace The Affected Flow

Review whichever complete path changed:

```text
CLI flags/path -> index load -> shared search/get -> formatter
MCP schema/semantic validation -> stable reload -> shared search/get -> SDK output
Markdown -> cards load/chunk -> index build/validate -> atomic write
```

Reject duplicated adapter behavior.

## Corpus Checks

- Reads remain limited to categorized card Markdown; configured index loading is a separate intentional file boundary.
- `.agents`/knowledge containment, symlink, non-regular, category, README, extension, and size rules remain intact.
- Duplicate IDs are detected across all statuses before inactive cards are skipped.
- `paths` remain citations only.
- Chunk title/headings/ordinals/line ranges remain deterministic.

## Index Checks

- Serialized changes include an explicit compatibility/version decision.
- Documents/chunks remain structurally consistent and root-bound.
- Loads reject unsafe, oversized, malformed, trailing, unsupported, and mismatched data.
- Atomic replacement preserves the old index on pre-rename failure.
- Modes and bounded writes remain secure.

## Search Checks

- Validation bounds and filter OR/AND semantics remain exact.
- Formula and reason order match docs/tests.
- One best chunk/card and score/source/ordinal tie ordering remain deterministic.
- Exact get remains case-sensitive.
- CLI and MCP outputs remain equivalent.

## CLI Checks

- Flags and positionals are strict.
- Default/custom path handling is consistent across commands.
- Success/error streams and exit 0/1/2 remain correct.
- JSON fields/error codes remain stable or are documented as contract changes.

## MCP Checks

- Exactly intended tools are exposed with typed closed schemas.
- Inputs cannot choose roots or write/index/shell.
- Semantic validation precedes reload.
- Fingerprint includes size, time, and identity; stable before/after load is required.
- Failed reload never serves stale data.
- Stdout cannot be contaminated.
- Cancellation, negotiation, tool calls, live reload, and EOF have protocol tests.

## Documentation Checks

- README/docs match executable behavior and distinguish unsupported HTTP/list/write operations.
- AGENTS/skills route future changes to the right owner and test boundary.
- Commands/examples are copy-paste correct and use current config schemas.

If no findings exist, state that explicitly and identify residual test or compatibility risk.
