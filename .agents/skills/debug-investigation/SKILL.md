---
name: debug-investigation
description: Use for bug reports, failures, regressions, unexpected ranking, malformed indexes, CLI errors, MCP connection failures, reload failures, or protocol lifecycle investigation in wfm-rag.
---

# Debug Investigation

Reproduce at the narrowest owning boundary and identify the violated contract. If the user requested remediation, add the failing test and make the smallest fix. For diagnosis-only requests, stop after evidence, root cause, impact, and recommended next action.

## Route By Symptom

| Symptom | Start at |
| --- | --- |
| Card missing/malformed/wrong chunks | `internal/cards` |
| Index missing/invalid/root/version/write preservation | `internal/index` |
| Wrong ordering/reasons/filter/excerpt/get | `internal/search` |
| CLI flags/output/error code/exit stream | `cmd/wfm-rag` |
| MCP schema/tool error/reload/cancellation | `internal/mcpserver` |
| Handshake/stdout/EOF/client integration | `cmd/wfm-rag/integration_test.go` and client config |

## Investigation Flow

1. Capture exact command/tool input, root, index path, output streams, and exit/error.
2. Reproduce with the CLI when possible to separate core/index failure from client transport.
3. Check the configured canonical root and whether default/custom index paths match.
4. Trace sentinels with `errors.Is`; do not classify by string when a sentinel exists.
5. Inspect score reasons before changing ranking.
6. For diagnosis-only work, stop after reporting evidence, root cause, impact, and recommended next action.
7. For requested remediation, reproduce in the focused package test and add a regression case for the observable failure and important recovery path.
8. Fix in the owning package.
9. Run adapter parity or subprocess tests only when that boundary is affected.

## Common Diagnoses

### Source edit not visible

Expected without reindex. Run `index`; there is no watcher.

### Index remains old after failed build

Expected atomic-preservation behavior. Fix the corpus/build error and retry.

### MCP reports reload failure

Inspect configured file for missing/malformed/root mismatch/unstable replacement. Do not fall back to stale memory.

### MCP has no tools

Run CLI search/get first, then inspect executable path, index path, client config shape, and stderr. Never add stdout logging to diagnose MCP.

### Wrong search result

Record normalized terms, filters, scores, reasons, source, heading, and ordinal. Determine whether the problem is card metadata/content or the shared formula. Add a deterministic expectation before changing weights.

### Get not found

Check exact case, active status, successful reindex, root/index match, and stable ID.

## Constraints

- Do not add logging to MCP stdout.
- Do not bypass validation, containment, bounds, atomic writes, or no-stale reload behavior to make a symptom disappear.
- Do not add interfaces/injection unless the failure cannot be exercised through existing boundaries.
- Do not modify the target repository while debugging implementation unless the user requested corpus changes.

## Verification

Run the focused owner test first, then:

```bash
go test -race -count=1 ./...
go vet ./...
```
