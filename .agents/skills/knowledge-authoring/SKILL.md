---
name: knowledge-authoring
description: Use when searching, adding, updating, drafting, activating, retiring, deleting, validating, listing, or evaluating repository knowledge cards and their retrieval quality.
---

# Knowledge Authoring

Knowledge Markdown is canonical; the JSON index is derived. Use MCP/CLI retrieval before manually scanning all cards when looking for existing guidance.

## Retrieval First

Before creating a card, search active indexed guidance by:

- likely developer wording;
- package/import name;
- cited path;
- candidate helper/identifier;
- broad scope and exact card ID.

Use `agent_knowledge_search` plus `agent_knowledge_get` when MCP is available, or CLI `search`/`get`. Then inspect source-card names/IDs for drafts and retired cards, which retrieval intentionally cannot see. Avoid duplicate guidance and duplicate IDs when an existing card can be updated.

## Discoverable Layout

Cards must be regular files with an exact lowercase `.md` extension under:

- `.agents/knowledge/patterns/`
- `.agents/knowledge/anti-patterns/`
- `.agents/knowledge/dependencies/`
- `.agents/knowledge/incidents/`
- `.agents/knowledge/sources/`

Nested directories are allowed. `README.md`, top-level knowledge docs, unknown categories, symlinks, and non-regular files are not indexed.

## Required Card Contract

- stable unique `id`, maximum 256 UTF-8 bytes;
- valid `type`: `pattern`, `anti-pattern`, `dependency`, `incident`, `reference`, `decision`;
- valid `status`: `draft`, `active`, `retired`;
- first H1 title.

Include `scope`, `triggers`, `packages`, `paths`, `source_confidence`, and `last_verified`. Use empty lists only for the four list fields; confidence and verification date are strings. Omitted optional fields warn.

IDs are unique across every status. Do not reuse retired IDs.

## Evidence And Citations

- `paths` are repository-relative citations and ranking hints.
- The RAG service never reads cited files; authors/reviewers must inspect evidence separately.
- Update `last_verified` only after checking evidence.
- Keep source confidence honest.
- Prefer exact package names, symbols, and likely query phrases.

## Lifecycle

1. Search active guidance and inventory inactive source IDs.
2. Create/update canonical Markdown.
3. Use `draft` until evidence and guidance are ready.
4. Validate through an isolated or normal `index` build.
5. Set `active`, reindex, and dogfood likely queries.
6. Retrieve full content and citations by ID.
7. Refresh evidence/content over time while preserving ID.
8. Set `retired` for historical but obsolete guidance; delete accidental/duplicate content.
9. Reindex after every status/edit/delete operation.

## Validation

There is no validate-only command. Use an isolated path when normal index replacement is undesirable:

```bash
check_dir="$(mktemp -d)"
wfm-rag index --root /absolute/repository --index "$check_dir/index.json"
```

A failed build preserves the prior normal index.

## Listing

There is no list command/tool. Inspect active documents with `jq` or source files for all statuses. Never treat blank search as listing.

## Retrieval Quality

- Add triggers for real language variants because there is no stemming/synonym expansion.
- Use package/path metadata for precise filtered retrieval.
- If ranking fails, record the query, expected ID/order, actual scores/reasons, and relevant corpus before proposing formula changes.
- Prefer improving card metadata/content when the formula is behaving as designed.
- Do not alter ranking without a focused test and CLI/MCP parity verification.

## Safety

- Do not edit generated index JSON.
- Do not make card changes in a target repository when the user asked only for RAG implementation changes.
- Keep implementation commits and target-corpus commits in their own repositories.
- Respect 1 MiB/card, 100 MiB corpus, 10,000-card, and 8,000-rune chunk limits.

See `docs/knowledge-authoring.md` for the full human contract.
