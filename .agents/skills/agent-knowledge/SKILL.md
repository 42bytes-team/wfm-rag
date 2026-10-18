---
name: agent-knowledge
description: Use for project knowledge retrieval and lifecycle work: search, get, add, update, draft, activate, retire, delete, validate, list, reindex, or evaluate RAG knowledge cards.
---

# Agent Knowledge

This is the portable `wfm-rag` skill for repositories that store cards under `.agents/knowledge/`. Keep the generic sections synchronized with the canonical skill and append repository-specific behavior only in the profile described at the end.

Knowledge Markdown is canonical. The JSON index is disposable derived state. Retrieval is RAG-first; do not manually grep or scan the card corpus as a substitute for `agent_knowledge_search` and `agent_knowledge_get`.

## Runtime Contract

- MCP is read-only and exposes `agent_knowledge_search` and `agent_knowledge_get`.
- A client may prefix those names with its configured MCP server name; use the tools ending in the canonical names.
- Every call names exactly one project. The `project` argument must be one of the IDs the MCP process was launched with; there is no corpus inference and no cross-corpus search.
- MCP cannot add, edit, retire, delete, list, validate, index, choose a root or index path, or run shell commands.
- Card writes/deletes use normal file tools or VCS; every lifecycle change is published with `wfm-rag index --project <id>`.
- A tool error naming `index_missing` is an operator problem: report it verbatim with its repair command. Do not retry the call with a different project.
- There is no HTTP transport.

## Project ID And CLI

Every command names a project. Read the project ID from this repository's Profile section below; if unknown, list registrations with:

```bash
wfm-rag project list
```

If this repository is not registered yet, register it once from its root (`wfm-rag project init <id>`), then index it. `init` also scaffolds the missing KB skeleton (category directories, the knowledge `README.md` carrying the profile-filling prompt, `schema.md`, card templates, and this starter skill with the project ID rendered in) — create-only, never overwriting existing files. Fill the starter skill's `Repository Profile` with that prompt before the first retrieval task. Never hard-code a machine path; the registry maps the ID to the canonical root, and the index path is derived from that root.

The default profile assumes `wfm-rag` is on `PATH` and all commands use the project's derived index. If the repository profile declares an absolute binary or `--index`, use it consistently for index/search/get/MCP.

## Mandatory Retrieval Workflow

Before source exploration or implementation when project knowledge may apply:

1. Search active cards with `agent_knowledge_search` using likely task wording.
2. Add exact package, cited path, type, or identifier terms when they are known.
3. Retrieve the complete best card with `agent_knowledge_get`.
4. Read only the card's cited source files or docs needed to verify decisive guidance.
5. Mention materially used card IDs in the final response.

Useful search dimensions:

- likely developer wording and synonyms already present in triggers;
- exact package/import name;
- touched or candidate source path;
- helper, parser, converter, symbol, error, or protocol name;
- broad scope and expected card type;
- exact stable card ID.

Do not begin by reading a hand-maintained knowledge index, grepping every card, or globbing the corpus. If MCP is unavailable, use the same RAG through CLI:

```bash
repo="$(git rev-parse --show-toplevel)"
cd "$repo" && wfm-rag search --project <id> "task wording package symbol"
cd "$repo" && wfm-rag get --project <id> card-id
```

If CLI fallback also fails, report the missing executable/index/configuration rather than silently reverting to corpus-wide manual retrieval.

## Discoverable Card Layout

Only regular files with an exact lowercase `.md` extension are candidates under:

- `.agents/knowledge/patterns/`
- `.agents/knowledge/anti-patterns/`
- `.agents/knowledge/dependencies/`
- `.agents/knowledge/incidents/`
- `.agents/knowledge/research/`
- `.agents/knowledge/sources/`
- `.agents/knowledge/state/`

Nested directories are allowed. Files named exactly `README.md`, files directly under `.agents/knowledge/`, unknown category directories, symlinks, and non-regular files are not indexed.

Pick the type by what the knowledge IS, then place it in the matching directory:

| Type | Directory | Purpose |
| --- | --- | --- |
| `reference` | `sources/` | Time-independent facts and external contracts — CLI surface, API behavior, tool formats |
| `decision` | `sources/` | Architectural decision and rationale |
| `pattern` | `patterns/` | A recommended workflow or implementation shape |
| `anti-pattern` | `anti-patterns/` | A recurring mistake to avoid, and why |
| `dependency` | `dependencies/` | An external tool or package and its non-obvious behavior |
| `incident` | `incidents/` | One tracked incident, open or closed: current state, cause and remediation, plus its concise history |
| `research` | `research/` | One tracked research target, open or concluded: current state and findings, plus its concise history |
| `state` | `state/` | Current project state: live inventories, pending work, open questions |

## What Goes Where

The KB is loaded into LLM context: cards must be compact and current.

Time-independent cards (`patterns/`, `anti-patterns/`, `sources/`, `dependencies/`) hold facts that stay true whenever they are read. NEVER store dated measurements (command output, counts, percentages, timestamps, or "confirmed case" data from one run), current project state, work-state narratives ("planned", "deferred", "kept as a worked example", "as of <date>"), incident retelling (incident card IDs, real identifiers, or one-off observations used as evidence — examples must be generic), or a correction banner over superseded text: rewrite the body so the card states only the corrected fact. Provenance markers ("verified live <date>") are allowed — they date the check, not the data. Sweep after card edits and periodically, and justify or remove every hit (`last_verified` and provenance markers are legitimate hits):

```bash
grep -rnE '20[0-9]{2}-[0-9]{2}-[0-9]{2}|[Cc]onfirmed case|as of|planned|deferred|kept as' \
  .agents/knowledge/patterns .agents/knowledge/anti-patterns \
  .agents/knowledge/sources .agents/knowledge/dependencies
```

State cards (`state/`) are dated snapshots, not live mirrors: current state, what is pending, and open questions, with no history for its own sake. Do not pin volatile counts in a card that is not the snapshot of record — describe the shape and point at the source of truth, and let the snapshot of record carry its date and a re-check command. Data that a database or service owns is not a card: store the query path, never a copy.

Incident and research cards own one tracked entry, open or closed, and its working directory: current state plus concise history, plus a description of any data blobs. A reusable lesson that outlives the entry graduates to a `patterns/` or `anti-patterns/` card; the entry card keeps the entry's own history. Scripts and large artifacts live in the working directory, never in the KB. One `state/tracked-work.md` card owns the list of currently open entries and points at each entry's card instead of describing it.

Re-check placement on every update: time-independent directories must not accumulate state, and `state/` must not accumulate history.

## Card Contract

```markdown
---
id: stable-kebab-case-id
type: pattern
status: draft
scope:
  - domain
triggers:
  - phrase an agent may search
packages: []
paths:
  - path/to/cited-evidence
source_confidence: high
last_verified: YYYY-MM-DD
---

# Clear Human Title

## Use When

When this guidance applies.

## Guidance

The preferred workflow or rule.

## Verify

- Evidence and commands to check.

## Avoid

Common wrong approaches and why.
```

Required and enforced:

- `id`: trimmed non-empty valid UTF-8, maximum 256 bytes, unique across every status;
- `type`: `pattern`, `anti-pattern`, `dependency`, `incident`, `research`, `state`, `reference`, or `decision`;
- `status`: `draft`, `active`, or `retired`;
- first H1 title.

Include `scope`, `triggers`, `packages`, `paths`, `source_confidence`, and `last_verified`. Use `[]` only for the four list fields. Confidence and date are strings. Omitted optional fields produce warnings.

Use H2 sections to create focused retrieval chunks. Keep important guidance after the H1. `paths` are citations and ranking hints; the RAG never opens cited files.

## Search Before Writing

Before adding a card:

1. Search active guidance through MCP for the concept, packages, paths, and candidate ID.
2. Get potentially overlapping cards and prefer updating one coherent card over duplication.
3. Inventory source IDs only to detect draft/retired collisions, because inactive cards are intentionally absent from retrieval. Include tracked and untracked candidate cards while excluding templates and README files:

```bash
repo="$(git rev-parse --show-toplevel)"
rg --hidden --no-ignore -n \
  --glob '**/{patterns,anti-patterns,dependencies,incidents,sources}/**/*.md' \
  --glob '!**/README.md' \
  "^[[:space:]]*['\"]?id['\"]?[[:space:]]*:" \
  "$repo/.agents/knowledge"
```

This targeted ID inventory handles ordinary quoted/unquoted YAML keys but is only a lifecycle aid, not the authority or a replacement for RAG retrieval. If an inactive card still cannot be located, inspect only candidate frontmatter blocks. The isolated full index build is the authoritative duplicate-ID and schema check.

## Add A Card

1. Verify cited evidence first.
2. Choose an allowed category and globally unique stable ID.
3. Create the card with `status: draft`.
4. Validate the complete corpus through an isolated index.
5. Fix every error and review warnings.
6. Change the card to `active` when evidence/guidance are ready.
7. Build the normal index.
8. Search realistic future wording and verify expected ordering/reasons.
9. Get the exact ID and verify complete content/citations.

```bash
repo="$(git rev-parse --show-toplevel)"
check_dir="$(mktemp -d)"
cd "$repo"

wfm-rag index --project <id> --index "$check_dir/index.json"
wfm-rag index --project <id>
wfm-rag search --project <id> "likely future query"
wfm-rag get --project <id> new-card-id
```

There is no validate-only command. The isolated full build is validation. The isolated `--index` file overrides only the file location; the corpus still comes from the registered project root. Remove the temporary directory afterward.

## Update A Card

1. Search/get the existing active card by topic or ID.
2. If the target is draft/retired or get returns not found, use the candidate-only ID inventory above and read only the exact matching source card.
3. Re-read cited evidence.
4. Edit only the canonical matching source.
5. Preserve the ID unless identity truly changes.
6. Update `last_verified` only after real verification.
7. Validate, reindex, search, and get again when active.

Source edits are invisible to retrieval until indexing succeeds.

## Retire A Card

Prefer retirement when historical rationale remains useful:

```yaml
status: retired
```

Reindex and confirm exact get returns not found. Retired cards remain parsed, validated, and ID-reserved.

## Delete A Card

Delete only accidental, duplicate, or intentionally removed source knowledge:

1. Search/get an active card and note its returned source path.
2. For a draft/retired card, locate its exact source with the candidate-only ID inventory and read only that match.
3. Confirm no durable rationale warrants retirement instead.
4. Delete that exact source through normal file/VCS tools.
5. Reindex successfully.
6. Confirm search/get no longer return it.

Deleting Markdown without a successful reindex leaves the previous indexed card visible.

## List And Status Limitations

There is no RAG list command. Search is relevance retrieval, not inventory, and blank queries are invalid.

When lifecycle work truly requires inventory:

- active cards: inspect `documents` in the generated index with `jq`;
- all statuses: inspect candidate source filenames/frontmatter IDs, including untracked drafts, with the constrained inventory command above.

Do not use inventory output as the normal context-retrieval path.

## Retrieval Quality Gate

- Use terms a future agent is likely to ask, not only internal jargon.
- Add real wording variants to triggers because retrieval has no stemming or synonyms.
- Use exact packages and useful repository-relative citations.
- Keep one coherent topic per card and concise H2 sections.
- Record query, expected ID/order, actual scores/reasons, and filters for misses.
- Prefer card metadata/content improvements when ranking behaves as designed.
- Do not change ranking formula from a target repository.

## Safety And Failure Behavior

- Never edit generated index JSON or the project registry JSON by hand; repair through `project init`, `project remove`, and `project healthcheck`.
- A failed build leaves the previous valid index untouched.
- IDs are unique within one project corpus; they carry no meaning across corpora.
- Only active cards are searchable/gettable.
- Reindex after add/update/activate/retire/delete.
- On `index_missing` or `project_unknown` from MCP or CLI, report the error and its repair command to the operator; do not guess a different project.
- Do not claim cited evidence was read unless it was actually inspected.
- Keep RAG implementation changes and target-repository card changes in their own repositories.

## Copy And Adapt

After copying this skill into another repository:

1. Route retrieval and card-lifecycle tasks to `agent-knowledge` in `AGENTS.md`.
2. Ensure `.agents/knowledge/` uses the seven supported categories.
3. Install/configure the `wfm-rag` binary; register the repository once with `wfm-rag project init <id>` and launch one MCP process with an explicit `--project` allowlist.
4. Append a short `Repository Profile` below including the project ID; do not fork the generic lifecycle unless the tool contract differs.
5. Restart the agent client because skills are loaded at startup.

Recommended profile fields:

```markdown
## Repository Profile

- Project ID: the registered `wfm-rag` project ID for this repository.
- Root: resolve with `git rev-parse --show-toplevel`.
- Binary: `wfm-rag` on `PATH`.
- Index: default derived path.
- MCP server: project-specific configured name with `--project <id>` in its launch arguments.
- High-value retrieval triggers: project-specific terms.
- Required evidence/test commands: project-specific commands.
```
