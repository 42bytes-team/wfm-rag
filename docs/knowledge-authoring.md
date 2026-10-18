# Knowledge Authoring And Lifecycle

## Source Of Truth

Knowledge lives as version-controlled Markdown in the target repository. Never edit the generated JSON index by hand.

```text
<repository>/.agents/knowledge/<category>/<card>.md
```

After every source change, run a complete `wfm-rag index` build.

A new KB bootstraps itself: `wfm-rag project init <id>` registers the project and creates the missing skeleton — the category directories, `.agents/knowledge/schema.md`, the eight card templates under `.agents/knowledge/templates/`, and a starter `.agents/skills/agent-knowledge/SKILL.md` with the project ID rendered in. Scaffolding is create-only: existing files are never overwritten, so a repository may keep its customized copies. Route matching tasks to the skill from `AGENTS.md`; keep repository-specific changes in the skill's final profile section so future synchronization stays easy.

## Discoverable Categories

Only these exact first-level directories are scanned:

```text
patterns/
anti-patterns/
dependencies/
incidents/
research/
sources/
state/
```

Nested directories are allowed. Discovery ignores:

- category and nested files named exactly `README.md`;
- non-lowercase `.md` extensions;
- files directly under `.agents/knowledge/`;
- unknown categories;
- symlinks and non-regular files.

The `.agents` and `.agents/knowledge` directories themselves must not be symlinks.

## Recommended Type Placement

| Type | Directory | Typical purpose |
| --- | --- | --- |
| `pattern` | `patterns/` | A recommended workflow or implementation shape |
| `anti-pattern` | `anti-patterns/` | A recurring mistake to avoid, and why |
| `dependency` | `dependencies/` | An external tool or package and its non-obvious behavior |
| `incident` | `incidents/` | One tracked incident, open or closed: current state, cause and remediation, plus its concise history |
| `research` | `research/` | One tracked research target, open or concluded: current state and findings, plus its concise history |
| `reference` | `sources/` | Time-independent facts and external contracts |
| `decision` | `sources/` | An architectural decision and its rationale |
| `state` | `state/` | Current project state: live inventories, pending work, open questions |

The implementation validates directory category and card type separately; this mapping is an authoring convention.

## Placement And Time Independence

Time-independent types stay time-independent. `patterns/`, `anti-patterns/`, `sources/`, and `dependencies/` hold facts that remain true whenever they are read: no dated measurements, no current project state, no work-state narratives, no incident retelling, and no correction banner over superseded text — rewrite the body instead.

`state/` holds dated snapshots of what is true now: what works, what is pending, open questions, and verification notes, with no history accumulated for its own sake. Do not pin volatile counts in a card that is not the snapshot of record; describe the shape, point at the source of truth, and let the snapshot of record carry its date and a re-check command. Data that a database or service already owns is not a card: the card stores the query path.

`incidents/` and `research/` hold one card per tracked entry, open or closed: current state, cause or findings, remediation, and a concise history, each card dated and linked to its own working directory. Scripts and large artifacts belong in that working directory, never inside a card. `state/tracked-work.md` lists only the currently open entries; a closed entry keeps its card and leaves the list. A reusable lesson that outlives its entry graduates to a `patterns/` or `anti-patterns/` card, so the entry card keeps only that entry's own history.

The target repository's `agent-knowledge` skill owns the detailed placement rules and the hygiene sweep.

## Complete Schema

```yaml
---
id: stable-kebab-case-id
type: pattern
status: draft
scope:
  - go
triggers:
  - phrase an agent may use
packages: []
paths:
  - src/path/to/evidence.go
source_confidence: high
last_verified: 2026-08-23
---
```

| Field | Required | Implemented validation | Authoring guidance |
| --- | --- | --- | --- |
| `id` | yes | Trimmed, valid UTF-8, non-empty, max 256 bytes, globally unique | Use stable kebab-case; do not recycle retired IDs |
| `type` | yes | Exact enum | Match the knowledge purpose |
| `status` | yes | `active`, `draft`, or `retired` | Draft before evidence is complete; retire stale guidance |
| `scope` | no | String list | Broad domains such as `go`, `config`, `auth` |
| `triggers` | no | String list | Concrete likely query wording |
| `packages` | no | String list | Exact imported module/package identifiers |
| `paths` | no | String list | Repository-relative citations and ranking hints |
| `source_confidence` | no | String | Conventionally `high`, `medium`, or `low` |
| `last_verified` | no | String | Conventionally `YYYY-MM-DD`; update only after checking evidence |

Unknown frontmatter fields are not currently rejected. Optional values are not deeply normalized or semantically validated, so author carefully.

Include every optional key. Use `[]` for intentionally empty lists. Omitted keys generate an indexing warning.

`paths` are not file-read permissions. They affect citations, path filters, and ranking only.

## Markdown Structure

The first level-one heading is required and becomes the card title:

```markdown
# Stable Human Title
```

Use level-two headings to create focused search chunks:

```markdown
## Use When

## Guidance

## Examples

## Verify

## Avoid
```

Those names are useful conventions, not parser requirements. H3 and lower headings remain in the surrounding H2 chunk.

Long H2 sections are split at paragraph boundaries and then hard-split if one paragraph exceeds 8,000 Unicode characters. Prefer short sections that answer one retrieval need.

Text before the first H1 is preserved by exact get but is not searchable. Put important guidance after the title.

## Writing Good Cards

- Capture durable repository knowledge, not temporary task notes.
- State when the card applies before implementation detail.
- Put the preferred action in direct language.
- Include exact package names, identifiers, and phrases developers actually use.
- Cite the strongest repository paths or authoritative references.
- Explain how to verify the guidance.
- Record dangerous alternatives in an `Avoid` section.
- Keep one coherent topic per card.
- Keep IDs stable when editing wording or citations.
- Never claim evidence that was not checked.

## Status Lifecycle

### Draft

Use `status: draft` while content is incomplete or unverified. Draft cards are fully parsed and validated but excluded from the index with a warning.

### Active

Use `status: active` only when the guidance is ready for search/get. Active cards become complete index documents and searchable chunks.

### Retired

Use `status: retired` when guidance should no longer be returned but historical source should remain. Retired cards are validated, skipped with a warning, and excluded from get/search after reindexing.

All statuses reserve their ID. A new card cannot reuse the ID of a draft or retired card.

## Add A Card

1. Select a category and stable ID.
2. Create the Markdown file.
3. Fill all metadata and at least one H1.
4. Start as draft if evidence remains incomplete.
5. Validate with an isolated or normal index build.
6. Activate and rebuild when ready.
7. Search using likely agent wording.
8. Retrieve by ID and inspect complete output/citations.

```bash
repo=/absolute/repository
project=api   # the registered project ID; `wfm-rag project list` shows registrations

wfm-rag index --project "$project"
wfm-rag search --project "$project" "likely wording package behavior"
wfm-rag get --project "$project" stable-card-id
```

## Update A Card

1. Re-read cited evidence.
2. Edit metadata/body while preserving identity.
3. Update `last_verified` only when verification happened.
4. Reindex.
5. Re-run representative searches.

Changing an ID is a breaking retrieval change: the old exact ID disappears and callers must use the new one.

## Retire A Card

Change the status and reindex:

```yaml
status: retired
```

Expected index warning:

```text
Warning: <source> (<id>): skipped card with status retired
```

Confirm that exact get now reports card not found.

## Delete A Card

Delete the canonical file through the target repository's VCS workflow and reindex:

```bash
git -C "$repo" rm .agents/knowledge/patterns/obsolete-card.md
wfm-rag index --project api
```

Deletion is not reflected until a successful rebuild replaces the old index.

Prefer retirement when historical rationale remains useful. Prefer deletion for accidental, duplicate, or never-valid content.

## Validate The Corpus

There is no validate-only command. Build an isolated index:

```bash
check_dir="$(mktemp -d)"

wfm-rag index \
  --project api \
  --index "$check_dir/index.json" \
  --format json
```

Successful indexing validates the complete candidate corpus, including inactive cards. Failure preserves any normal existing index because this command used a separate path.

## List Cards

There is no built-in list operation.

List active indexed cards:

```bash
index_path="$(
  wfm-rag index --project api --format json |
    jq -r '.index'
)"

jq -r '.documents[] | [.id, .type, .source] | @tsv' "$index_path"
```

List tracked source files, including draft and retired cards, from version control:

```bash
git -C "$repo" ls-files '.agents/knowledge/**'
```

This may also show non-card knowledge documentation such as `README.md` or schema files. The JSON index intentionally contains only active cards.

## Reindex And Publish

Every index command is a complete build. There is no incremental mode.

Default path:

```bash
wfm-rag index --project api
```

Explicit path:

```bash
index=/absolute/index.json

wfm-rag index --project api --index "$index"
wfm-rag search --project api --index "$index" "query"
```

`mcp` accepts no `--index`: it always uses the derived path of each selected project.

```bash
wfm-rag mcp --project api
```

Every process must select the same project. `--index` relocates the index file only; the expected root stays the project's registered canonical root.

## Limits

| Limit | Value |
| --- | ---: |
| Candidate file | 1 MiB |
| Candidate source corpus | 100 MiB |
| Candidate cards | 10,000 |
| ID | 256 UTF-8 bytes |
| Search chunk | 8,000 Unicode characters |
| Stored index | 512 MiB |

Inactive cards count toward file, corpus, and card-count limits.

## Review Checklist

- File is under an allowed category and is not named `README.md`.
- ID is stable and unique across every status.
- Type and status are valid.
- Optional metadata is explicitly present.
- First H1 is a clear title.
- H2 sections separate retrieval concerns.
- Triggers resemble likely developer queries.
- Packages use exact identifiers.
- Paths are relevant repository-relative citations.
- Guidance is supported by cited evidence.
- `last_verified` reflects real verification.
- Isolated or normal indexing succeeds.
- Representative search ranks the card appropriately.
- Exact get preserves full content and citations.
