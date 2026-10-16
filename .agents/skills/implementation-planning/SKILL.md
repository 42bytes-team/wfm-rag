---
name: implementation-planning
description: Use for planning, design, scoping, research, comparison, or proposed architecture and behavior changes in wfm-rag before implementation begins.
---

# Implementation Planning

Planning may inspect source/tests/docs and update `docs/planning/`, but must not edit production code or tests until execution is approved.

## Start With Ownership

Use `AGENTS.md` and `docs/architecture.md` to identify the owner. Do not read the entire historical implementation plan by default.

Plan-section routing:

- corpus and chunking: implementation plan sections around card schema/discovery;
- index format/persistence: index design section plus `docs/index-format.md`;
- ranking/filtering: search section plus dogfood report;
- CLI/MCP: command/tool sections plus `docs/cli.md` and `docs/mcp-clients.md`;
- testing/errors: testing and error-contract sections.

Current source/tests override stale examples in a historical plan.

## Workflow

1. State the user-visible problem and measured evidence.
2. Identify owning package and current public contract.
3. Read focused tests and affected docs.
4. Trace both CLI and MCP if shared retrieval changes.
5. Verify pinned dependency APIs when relevant.
6. Propose the smallest design preserving deterministic parity.
7. Name exact tests, failure paths, operational risk, and rollback.
8. Record unresolved decisions; do not hide ambiguity.

## Impact Checklist

Every plan must state whether it changes:

- card discovery, schema, status lifecycle, or limits;
- serialized index fields/validation and `index.Version`;
- normalization, scoring weights, reason order, filters, ties, or excerpts;
- CLI flags, JSON fields, error codes, streams, or exit status;
- MCP tool names, schemas, structured output, reload, or protocol compatibility;
- client configuration/documentation;
- containment, file permissions, bounds, or determinism.

## Ranking Changes

Do not tune ranking from intuition. Record a real failed query/card expectation, name the failing test to add before formula changes during execution, and assess CLI/MCP parity. Prefer card metadata/content improvements when the problem is corpus wording rather than the formula.

## Scope Guardrails

Reject speculative databases, HTTP transport, embeddings, watchers, background workers, multi-project servers, interfaces, and dependency injection. Consider them only after measured v0.1 behavior requires them and the user approves scope.

## Documentation

Planning docs record decisions. Public operation belongs in README and focused docs; agent workflow belongs in AGENTS/skills. Include documentation updates in every behavior-changing plan, and name the affected `README.md` section (Install, Onboarding, Connect An LLM Through MCP, CLI Reference, Knowledgebase) plus the focused `docs/` owner in the impact checklist. AGENTS.md "Documentation Sync" maps each change type to both.
