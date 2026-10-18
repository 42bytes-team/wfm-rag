---
name: agent-knowledge
description: Load first in every {{PROJECT}} session and before any repository read, command, plan, delegation, or conclusion. Search this project's knowledge base for durable facts, prior findings, workflow lessons, and current project state.
---

# Agent Knowledge

**This is the FIRST skill to load in any session.** Before reading files,
running commands, or investigating anything — search the KB. The KB is the
project's memory: durable facts, prior findings, analysis lessons, documented
workflows, safety rules, and prior decisions. Skipping this step means
re-deriving known facts, repeating work that was already done, and wasting
context.

Use the KB before relying on memory, guessing dependency or tool behavior, or
re-deriving a fact the project already recorded. The KB is where this project's
durable facts, decisions, and prior findings live; verify decisive guidance
against the card's cited source files or tests before acting on it.

## Retrieval

Use the **configured `wfm-rag` MCP server** for this repository and select it
with `"project": "{{PROJECT}}"` in every call.

- `agent_knowledge_search` — search active cards of the selected project.
- `agent_knowledge_get` — fetch one complete card by its stable `id`.
- A client may prefix those names with its configured MCP server name; use the
  tools ending in the canonical names.
- MCP is read-only: it cannot add, edit, retire, delete, list, validate, index,
  choose a root or index path, or run shell commands. Card writes use normal
  file tools; every lifecycle change is published with
  `wfm-rag index --project {{PROJECT}}`.

**Wrong-project guard:** the search response `root` is the selected project's
registered canonical root. If it is not this repository's
`git rev-parse --show-toplevel`, the wrong project was selected or the server is
misconfigured — do not trust the result; report it and fall back to the CLI. An
empty or narrowly matched result set is **not** evidence of a wrong project:
retrieval is lexical, with no stemming or synonyms, so retry with concrete terms
first.

If the MCP server is not configured, or the guard trips, use the `wfm-rag` CLI
(`project {{PROJECT}}` is registered in the local registry; put the query
**last**, quoted, flags before it):

```bash
wfm-rag search --project {{PROJECT}} --limit 5 "query here"
wfm-rag get --project {{PROJECT}} card-id
```

A tool error naming `index_missing` or `project_unknown` is an operator problem:
report it verbatim with its repair command (`wfm-rag index --project {{PROJECT}}`
or `wfm-rag project list`). Do not retry the call with a different project.

## Retrieval Preflight

A preflight is complete only when you:

1. Search with the user's domain nouns and intended operation.
2. Verify that `root` is this repository and fetch each relevant card in full
   with `agent_knowledge_get`.
3. Record the query, card IDs, and each card's `last_verified` in the working
   note or final report.
4. If results are empty or narrowly matched, retry with concrete filenames,
   identifiers, CLI flags, status codes, and common synonyms.
5. Repeat the preflight when the task materially changes scope.

Search before ANY work — before opening a file, running a command, writing a
function, debugging, or repeating a documented workflow. If a card exists,
**follow it**: do not re-derive what is documented, do not create duplicate
helpers or scripts, and do not re-explore structure the KB already maps.

**NEVER reimplement project capability.** Before writing any ad hoc script,
parser, or batch pipeline, the preflight must rule out an existing project
command, a KB-documented workflow, and an existing artifact. If the project can
already do the operation, use the project path — a throwaway reimplementation
of existing capability is a preflight failure, not a workaround.

## Card Types

Pick the type by what the knowledge IS, then copy the matching template from
`.agents/knowledge/templates/`. Required frontmatter and body sections are in
`.agents/knowledge/schema.md`.

| Type | Purpose (when to use) | Directory | Template |
|---|---|---|---|
| `reference` | Time-independent facts and external contracts — layout, CLI surface, API behavior, tool formats | `sources/` | `reference.md` |
| `decision` | An architectural decision and its rationale | `sources/` | `decision.md` |
| `pattern` | A recommended workflow or implementation shape | `patterns/` | `pattern.md` |
| `anti-pattern` | A recurring mistake to avoid, and why | `anti-patterns/` | `anti-pattern.md` |
| `dependency` | An external tool or package and its non-obvious behavior | `dependencies/` | `dependency.md` |
| `incident` | One tracked incident, open or closed: current state, cause and remediation, plus its concise history | `incidents/` | `incident.md` |
| `research` | One tracked research target, open or concluded: current state and findings, plus its concise history | `research/` | `research.md` |
| `state` | Current project state — live inventories, pending work, open questions | `state/` | `state.md` |

## What Goes Where

The KB is loaded into LLM context: cards must be compact and current.

**Time-independent knowledge** — `patterns/`, `anti-patterns/`, `sources/`,
`dependencies/`: store only facts, guidance, hints, and data that are not tied
to a specific point in time. NEVER store changelogs, current project state,
historical data points, dated operator decisions, or references into past
incidents (point at the incident card; do not retell it). If a fact is
"as of <date>" or describes what something is doing right now, it does not
belong in these directories.

Hard rules for time-independent cards:

- No dated measurements: no command output, counts, percentages, timestamps, or
  "confirmed case" data from a specific run. A number from one run is a
  historical data point, not a fact.
- No incident retelling: no incident card IDs, real identifiers, cohorts, or
  one-off observations used as evidence for a claim. Examples must be generic
  and obviously illustrative ("user X", "IP X"). A bare pointer ("see the
  `<incident>` card") is allowed only when the incident is not the subject of
  the sentence.
- Never paste a correction banner ("Superseded (date): the procedure below is
  wrong") over superseded text. When a claim is corrected, rewrite the body so
  the card states only the corrected fact. A card whose banner contradicts its
  own body is a hygiene failure — the wrong text stays loaded into every
  session that retrieves it.
- No work-state narratives: "X is planned", "deferred", "kept as a worked
  example", "as of <date> it was failing". The card states the rule; current
  work state lives in `state/` and in the incident/research cards.
- Provenance markers ("verified live <date>", a link to a planning document)
  are allowed: they date the check, not the data.
- Sweep after card edits and periodically; justify or remove every hit
  (frontmatter `last_verified` and provenance markers are legitimate hits):

  ```bash
  grep -rnE '20[0-9]{2}-[0-9]{2}-[0-9]{2}|[Cc]onfirmed case|as of|planned|deferred|kept as' \
    .agents/knowledge/patterns .agents/knowledge/anti-patterns \
    .agents/knowledge/sources .agents/knowledge/dependencies
  ```

**Current project state** — `state/`: what works, what is pending, quirks,
unfinished tasks, open questions, and useful verification notes. No history for
its own sake — keep a historical fact only when a pending question or an
unfinished feature needs it.

State-card hygiene: state cards are dated snapshots, not live mirrors. Do not
pin volatile counts (record counts, list sizes, slot counts, item counts) in a
card that is not the snapshot of record — describe the shape and point at the
source of truth (CLI, database, service). The snapshot of record carries its
date and a re-check command. Change-history narratives ("was X", "absorbs the
old Y", version histories) do not belong in state cards — describe what exists
now and why. Data that a database or service already owns is not a KB card:
store the query path, never a copy.

**Ownership of tracked work:** one `state/tracked-work.md` card owns the *list*
of currently open incidents and research targets (card, one-line status, working
directory). It must not describe individual entries — point at the entry's card.
A closed entry keeps its card and leaves the list. Each incident or research
card owns its own entry and its working directory.

**Tracked incidents and research** — `incidents/`, `research/`: one card per
tracked entry, open or closed, always linked to its working directory. Store
the current state (status, control in place and working or not, what is
monitored, where the tooling lives) plus a concise history: timeline, cause,
findings, hypotheses, quirks, theories — everything useful while the entry is
open, and everything a later reader needs once it is closed. Describe data
blobs (what, why, how long they stay relevant). NEVER store data blobs or
scripts in the KB — everything large goes into the working directory. A
reusable lesson that outlives the entry graduates to a `patterns/` or
`anti-patterns/` card; the entry card keeps the entry's own history.

## Update Rules

- Search before writing: look up the concept, packages, paths, and candidate ID
  through MCP, then prefer updating one coherent card over creating a
  near-duplicate.
- Add a new card only when the knowledge is distinct and reusable.
- Use the templates in `.agents/knowledge/templates/`.
- `status: active` = verified and safe to rely on. `draft` = unverified **and
  not indexed** (the indexer skips `draft` cards). If knowledge is useful but
  incomplete, set it to `active` and mark the unknowns as `Open question (gap)`
  inside the card — a hidden `draft` card is less useful than a visible
  `active` card with marked gaps.
- Keep cards specific and self-contained: one coherent topic per card, concise
  H2 sections, triggers phrased as wording a future agent would actually type.
- **Place it in the right section** — follow "What Goes Where" above. When
  updating a card, re-check that its content still belongs in its directory:
  time-independent directories must not accumulate state, and `state/` must not
  accumulate history.
- Validate before activating. There is no validate-only command; an isolated
  full build is the validator and never touches the live index:

  ```bash
  check_dir="$(mktemp -d)"
  wfm-rag index --project {{PROJECT}} --index "$check_dir/index.json"
  rm -rf "$check_dir"
  ```

- After a discovery or any card edit, run `wfm-rag index --project {{PROJECT}}`
  to refresh the index, then search realistic future wording and get the exact
  ID. A change without a KB update is **incomplete**.
- Prefer `status: retired` over deletion when the historical rationale still
  matters; delete only accidental or duplicate cards. Reindex after every
  lifecycle change. Never edit the generated index JSON or the project registry
  by hand.
- When a KB change makes a duplicated claim in a skill or tracked project
  document stale, update that claim in the same change or label it explicitly
  as historical with its version/date and a pointer to the active card. Never
  leave two current-looking versions of the same fact.

## Repository Profile

Fill this section in whenever the skill is copied into a repository. `project
init` leaves the last two bullets as placeholders; `.agents/knowledge/README.md`
carries the prompt that fills them. Keep every repository-specific rule here and
never fork the generic sections above.

- Project ID: `{{PROJECT}}`.
- Root: resolve with `git rev-parse --show-toplevel`.
- Binary: `wfm-rag` on `PATH`.
- Index: default derived path.
- MCP server: the configured server name with `--project {{PROJECT}}` in its
  launch arguments.
- High-value retrieval triggers: <project-specific terms>
- Required evidence and verification commands: <project-specific commands>

## Commit

Commit knowledge changes with the repository's documentation scope for agent
knowledge:

```text
docs(agents): add <topic> card
docs(agents): update <card-id> with <what changed>
```

## Finish Criteria

- The relevant card was searched before the work.
- Any new or changed knowledge was captured in a card with honest status and confidence.
- Every card add, edit, activation, retirement, or deletion was published with
  `wfm-rag index --project {{PROJECT}}`; a retrieval-only session needs no reindex.
