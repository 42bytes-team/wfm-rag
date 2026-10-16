# Knowledge Card Schema

Every card is Markdown with YAML frontmatter and a first H1 title. This
project is registered as `{{PROJECT}}` in the local wfm-rag registry.

## Storage

Place cards only beneath:

```text
.agents/knowledge/patterns/
.agents/knowledge/anti-patterns/
.agents/knowledge/dependencies/
.agents/knowledge/incidents/
.agents/knowledge/research/
.agents/knowledge/sources/
.agents/knowledge/state/
```

Nested directories are allowed. Files directly under `.agents/knowledge/`,
unknown categories, symlinks, non-regular files, category `README.md`, and
extensions other than exact lowercase `.md` are not indexed.

## Type Placement

| Type | Directory | Typical purpose |
| --- | --- | --- |
| `reference` | `sources/` | Time-independent facts and external contracts |
| `decision` | `sources/` | An architectural decision and its rationale |
| `pattern` | `patterns/` | A recommended workflow or implementation shape |
| `anti-pattern` | `anti-patterns/` | A recurring mistake to avoid, and why |
| `dependency` | `dependencies/` | An external tool or package and its non-obvious behavior |
| `incident` | `incidents/` | Current state of one ongoing incident plus its concise history |
| `research` | `research/` | Current state of one ongoing research target plus its concise history |
| `state` | `state/` | Current project state: live inventories, pending work, open questions |

Directory and type are validated separately; this mapping is the authoring
convention. Copy the matching file from `.agents/knowledge/templates/` instead
of writing frontmatter from memory.

## Frontmatter

```yaml
---
id: short-stable-id
type: pattern
status: draft
scope: []
triggers:
  - helper
  - parser
packages: []
paths:
  - path/to/cited-evidence
source_confidence: high
last_verified: YYYY-MM-DD
---
```

## Fields

- `id`: required trimmed non-empty valid UTF-8 identifier, maximum 256 bytes, unique across active, draft, and retired cards. Use kebab-case as the repository authoring convention.
- `type`: required; `pattern`, `anti-pattern`, `dependency`, `incident`, `research`, `state`, `reference`, or `decision`.
- `status`: required; use `draft` for unverified work, `active` for retrievable guidance, and `retired` for stale guidance kept for history.
- `scope`: repository layer, app, language, or task category.
- `triggers`: exact user-facing terms that should retrieve the card. Include domain aliases, concrete filenames, relevant CLI flags, and common phrasings; search is lexical and supplies no stemming or synonym expansion. Before activating or materially updating a card, run at least two representative natural-language searches and confirm that the card is returned.
- `packages`: modules, packages, services, protocols, or external tools involved.
- `paths`: repository-relative citations and path-ranking hints. They are not instructions for `wfm-rag` to read those files.
- `source_confidence`: authoring convention `high`, `medium`, or `low`.
- `last_verified`: `YYYY-MM-DD` for the whole card. Advance it only after checking every current-state assertion in the card against evidence. If only one section was re-verified, date that section locally and leave the card-level date unchanged.

Include every optional field. Use `[]` only for empty `scope`, `triggers`, `packages`, or `paths`. Confidence and verification date are strings. Omitted optional fields produce warnings.

## Body

```markdown
# Title

## Use When

When this card is relevant.

## Guidance

The preferred workflow or rule.

## Examples

Short local examples or references.

## Verify

Commands, docs, or source files to check.

## Avoid

Common mistakes and why they are wrong.

## Notes

Optional nuance, exceptions, or follow-up work.
```

The first H1 is required and becomes the title. H2 sections become search
chunks; H3 and lower headings remain in their surrounding H2. Keep one
focused retrieval concern per section.

## Time Independence

`patterns/`, `anti-patterns/`, `sources/`, and `dependencies/` hold facts that
stay true regardless of when they are read: no dated measurements, no
work-state narratives, no incident retelling, no correction banners over
superseded text. `state/`, `incidents/`, and `research/` hold dated snapshots:
current state plus concise history, each card tied to its date and to its
source of truth or working directory.

The full placement rules and the hygiene sweep live in the `agent-knowledge`
skill, "What Goes Where". A card that violates them is reclassified or trimmed,
not annotated.

## Implemented RAG Behavior

- Only active cards enter search/get documents and chunks.
- Draft and retired cards are still parsed, validated, and checked for duplicate IDs.
- Search indexes frontmatter metadata and Markdown chunks.
- `paths` are returned as citations and used by path filters/ranking, but cited files are never ingested.
- Search is deterministic lexical retrieval with no stemming, synonym expansion, embeddings, or semantic reranker.
- Source changes are invisible until `wfm-rag index --project {{PROJECT}}` succeeds.
- A failed build preserves the previous valid index.
