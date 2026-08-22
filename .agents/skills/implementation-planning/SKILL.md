---
name: implementation-planning
description: Use for planning, design, scoping, research, comparison, or proposed architecture changes in wfm-rag before implementation begins.
---

# Implementation Planning

Planning may inspect files and update documents under `docs/planning/`, but must not edit production code or tests until execution is explicitly approved.

## Workflow

1. Read `docs/planning/agent-rag-mcp-implementation.md`.
2. Identify the exact CLI, index, search, or MCP contract affected.
3. Verify dependency APIs against pinned versions.
4. Prefer the smallest design that preserves deterministic CLI/MCP parity.
5. Name tests, operational risks, and rollback.
6. Record unresolved decisions in the plan.

Reject speculative scale work such as databases, embeddings, HTTP transport, watchers, or multi-project servers unless measured v0.1 behavior requires it.
