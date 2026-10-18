# Project-Scoped Knowledge Base: 2026-10-08 (rev 2, 2026-10-09)

## Status

Implemented 2026-10-09. Approved after review and carried out end to end: `internal/project`, the state-directory resolver in `internal/index`, single-index `internal/search`, project-keyed `internal/mcpserver`, and the `cmd/wfm-rag` cutover with the `project` subcommand group. All docs listed under Change Impact are updated.

Review 1: Sol GPT 5.6, 2026-10-08, approve with changes; corrections applied in rev 1.
Review 2: parent assistant, 2026-10-09, approve with changes; findings and operator decisions applied in this revision (see Review Record). No open decisions remain.

This plan supersedes the cross-corpus merging part of [`agent-context-cost-multi-root-mcp.md`](agent-context-cost-multi-root-mcp.md). That plan's substrate — one process holding several validated indexes — is kept. Its retrieval semantics (merge every configured corpus into one ranked list) are replaced by explicit per-project selection.

## Problem

The operator runs four corpora: `Api`, `wf-unpacker`, `Manager`, `Cloudflare`. Both configurations tried so far have a defect.

1. One MCP server per repository. Retrieval is isolated and IDs never collide, but every server publishes its own copy of both tool definitions into every model request. Measured in the operator's OMP session: four knowledge servers cost 828 input tokens per request, and a cross-repository question depends on the agent choosing the right server name.
2. One MCP server with four `--root` values (implemented 2026-10-08, uncommitted). One tool pair, about 380 tokens saved per request, cross-corpus search works — but card IDs share one namespace. The operator's own corpora produced five real collisions: `agent-working-conventions` (`Api` ↔ `wf-unpacker`), `project-directory-reference`, `project-purpose`, and `wfm-database` (`wf-unpacker` ↔ `Manager`), and `project-state` (`wf-unpacker` ↔ `Cloudflare`). Merged limits gave 5 of 6 result slots to one corpus for the query `config`.

The fix is to make the caller name the project. One process can still serve every corpus, but each call reads exactly one, chosen before any index is touched.

## Decisions Fixed By The Operator

1. All persistent state — the existing index files and the new registry — lives in `$XDG_DATA_HOME/wfm-rag/` when `$XDG_DATA_HOME` is set and absolute, and in `$HOME/.local/share/wfm-rag/` otherwise. The hardcoded path is removed from existing functionality too; see State Directory.
2. There is no repository marker. Registration is always explicit: `wfm-rag project init <id>`. Nothing is written into target repositories. Identity lives only in the registry.
3. Every retrieval call names a project explicitly. There is no inference from the current directory, for the CLI or for MCP. A missing or unknown project is a rejection that lists the projects that process can serve.
4. One project per call. Cross-project questions are separate calls.
5. Commands are `wfm-rag project init`, `wfm-rag project remove`, and `wfm-rag project list`.
6. One project cap everywhere: 64 projects in the registry and at most 64 projects served by one MCP process. A 16-project process cap was rejected as a pointless desync; these corpora are small.
7. An MCP server starts even when a configured project's index is missing or broken. The launch prints a stderr warning, and every call that selects that project returns an actionable error the model must relay to the operator. See Degraded Projects.
8. `wfm-rag project healthcheck` is added: a CLI-only maintenance sweep over registry liveness, index presence, and dangling index directories, with deletion only under an explicit `--purge`. See Project Healthcheck.

## Design

### State Directory

```text
stateDir():
    if $XDG_DATA_HOME is set and absolute:  base = $XDG_DATA_HOME/wfm-rag
    else:                                   base = $HOME/.local/share/wfm-rag
```

This replaces the hardcoded `~/.local/share/wfm-rag` in `index.DefaultPath` and is also the location of the new registry. Per the XDG Base Directory spec, a relative `$XDG_DATA_HOME` is invalid, so the fallback applies; an empty or unset variable also falls back. The state directory is created `0700` on demand.

Behavior change and migration: when `$XDG_DATA_HOME` is unset (the common case), nothing moves. When it is set, `index.DefaultPath` resolves elsewhere and existing indexes under `~/.local/share` become invisible. There is no fallback read and no migration shim — the operator re-runs `index` once per project. Index rebuilds are local and cheap. `AGENTS.md` currently states "`$XDG_DATA_HOME` is not used"; that invariant line is rewritten by this plan (see Agent-Contract Lines).

### Registry

```text
<stateDir>/projects.json
```

Versioned identity map, and nothing else:

```json
{
  "version": 1,
  "projects": {
    "api": { "root": "/home/kyckyc/Projects/Warframe/Services/Api" },
    "cloudflare": { "root": "/home/kyckyc/Projects/Cloudflare" }
  }
}
```

Only the canonical root is stored. Index paths are derived by `index.DefaultPath`; limits, timestamps, and per-project overrides are not persisted, because they already have authoritative owners elsewhere and duplicating them creates drift.

Rules:

- Project ID: `[a-z0-9][a-z0-9-]{0,31}`. Short because it appears in tool schemas, error messages, and shell commands.
- Stored root: non-empty absolute path. Relative, empty, or whitespace values are rejected at parse time, so a hand-edited bad value fails at validation instead of surfacing late inside `index.DefaultPath` or `project list`.
- File: regular file, mode `0600`, parent state directory `0700`, symlinks and non-regular files refused, at most 1 MiB and at most 64 projects.
- Parsing is strict: trailing data rejected, duplicate object keys rejected through token-level detection rather than a plain map unmarshal, unknown fields rejected, output always rendered with projects sorted by ID.
- Writing is serialized by `flock(LOCK_EX | LOCK_NB)` on `<stateDir>/projects.lock` (mode `0600`). The writer re-reads the registry under the lock, writes a temporary sibling, syncs, closes, renames, syncs the parent directory, and releases the lock. Atomic rename alone is not concurrency control; without the lock, two concurrent `project init` and `project remove` calls can lose one update.
  - `flock` is required rather than an `O_CREATE|O_EXCL` lockfile: the kernel releases an `flock` lock when the holding process exits or crashes, so no stale lock can survive and permanently block registration. An exclusive-create lockfile would do exactly that.
  - A held lock is not waited on: the second writer exits 1 with `project_locked` naming the lock file. Registry writes are rare and interactive; a clear failure beats a silent wait.
- Structural validity and liveness are separate checks. A structurally valid registry whose registered root vanished must still support `list`, `remove`, and `init` repair. Liveness is checked only for the project a command actually selects, and `index.Load` continues to enforce root binding.

### Repository Marker — Rejected

The 2026-10-08 draft defined a committed `<repository>/.wfm-rag` marker file. It is dropped. Sol GPT 5.6 objected that a committed alias duplicates identity state and makes forks, clones, and `git worktree` directories inherit an ID the registry binds to one path; the second review added that the marker was the only source of a contradictory `project_marker_mismatch` exit-code contract and the only reason the tool ever wrote into a foreign repository. Removing it deletes a state location, an error code, a write surface, a test block, and the fork/worktree rules, at the cost of retyping one slug during `project init`.

### Commands

```text
wfm-rag project init <id> [--dir <path>] [--format text|json]
wfm-rag project remove <id> [--format text|json]
wfm-rag project list [--format text|json]
wfm-rag project healthcheck [--verify] [--purge] [--format text|json]
```

`project init` is idempotent:

- The ID is not registered: ensure the state directory and register `id -> canonical root` of `--dir` or the working directory.
- The ID is registered to a different directory: rebind to this directory and report the previous root. Rebinding is never implicit; only `init` performs it, it is reported in the output, and `project list` shows the current binding. This is how a moved checkout or a new worktree is repaired.
- The ID is already registered to this directory: no-op refresh.

`project remove` deletes only the registry entry. It never touches repository files or index files.

`--dir` is accepted only by `project init`. Retrieval and build commands never take a directory: `--project` is the sole locator of a corpus, resolved through the registry. Allowing `--dir` on `index` would create a second locator, reintroducing the `--root` bypass this plan retires, and would produce a self-contradictory artifact — an index built from a directory other than the project's registered root stamps that other root and is then rejected by `index.Load` under the same project ID.

`project list` is a cheap registry view: ID, canonical root, derived index path, and `root missing` or `index missing` status. It does not open index files; loading a 512 MiB index to print a generation timestamp is not an identity command. Deep verification and whole-state cleanup belong to `project healthcheck`, below.

### Retrieval Contract

```text
wfm-rag index  --project <id> [--index <file>] [--format text|json]
wfm-rag search --project <id> [--index <file>] [--path <path>] [--package <pkg>] [--type <type>] [--limit 5] [--format text|json] <query>
wfm-rag get    --project <id> [--index <file>] [--format text|json] <card-id>
wfm-rag mcp    --project <id> ...   # or: wfm-rag mcp --all-projects
```

`--root` is retired, so the registry cannot be bypassed and the repeatable-flag pairing rules from the previous plan disappear.

#### What `--index` overrides

Every index file records the canonical root it was built from, and `index.Load(path, expectedRoot)` refuses any file whose recorded root differs from the expected root (`index.ErrRootMismatch`, `internal/index/index.go:201`). That check is what makes an index file for project `B` unusable when project `A` was selected.

`--index <file>` changes only which file the command reads. It never changes the expected root, which is always the selected project's registered canonical root:

```text
wfm-rag index  --project api --index /tmp/ci/index.json      # builds from api's registered root,
                                                              # writes to the chosen file
wfm-rag search --project api --index /tmp/ci/index.json "query"
#   reads /tmp/ci/index.json, still requires the file to claim api's registered root
```

So `--index` relocates the index file; it does not substitute a different checkout. A CI job that clones the repository to a different path gets a different canonical root, and its index is rejected under `--project api`. CI scratch builds work when the build runs at the registered root path, or when the CI checkout is registered as its own project (`project init ci-api --dir /build/checkout`). `--index` is not stored in the registry and is not accepted by `mcp`.

`mcp` requires at least one `--project`. Running `wfm-rag mcp` with none exits 2 with `project_required`. Registry membership does not silently grant every client access to a corpus added later; the launch arguments are the allowlist. `--all-projects` is an explicit opt-in that selects the whole registry, capped at 64 projects exactly as the registry itself is. More than 64 explicit `--project` flags exits 2 with `project_limit_exceeded`. Startup fails cleanly if the resulting set is empty.

MCP tools stay exactly two, with separate closed-world input envelopes rather than a project field inside `search.Query`:

```text
agent_knowledge_search { project, query, paths?, packages?, types?, limit? }
agent_knowledge_get    { project, id }
```

`project` is a required string whose JSON schema carries an enum of the IDs this process was launched with, sorted. The enum is generated from the launch allowlist, not from every registry entry, so a restricted server never advertises corpora it cannot serve. Runtime validation stays as defense in depth, and a rejected value returns `project_unknown` listing only the configured IDs in sorted order.

Server-side state becomes a project-keyed map instead of an ordered merge slice. A call resolves its project first, then stats, reloads, and loads only that project's index. A broken or missing index for project `B` must not fail a call that selected project `A`.

#### Launch-time unknown project

There are two distinct moments a project name can be wrong, and they fail differently:

- At server launch: `wfm-rag mcp --project cloudfire` (typo, or a project removed from the registry). The flag names a project the registry cannot resolve. The server exits 2 immediately with `project_unknown` listing the registry's IDs, and never starts. Failing at launch is correct because a server that started anyway would advertise a dead enum entry and every call against it would fail — the operator would learn about the typo only through tool errors.
- At call time: the tool input names a project outside the launch allowlist. The server is healthy; the call is rejected with `project_unknown` listing the configured IDs, and no index is touched.

#### Degraded projects

At launch, every allowlisted project is resolved through the registry (unknown → exit 2, above), and each project's index is attempted. An index that is missing or invalid does not abort startup:

```text
warning: project "cloudflare": no valid index at <stateDir>/6f2a…/index.json;
         calls selecting it will fail until `wfm-rag index --project cloudflare` succeeds
```

The server starts, the degraded project stays in the tool enum, and a call that selects it re-attempts the load (select-then-reload). If it still fails, the tool result is an error whose text is written for the model to relay:

```text
index_missing: project "cloudflare" has no valid index.
Ask the operator to run: wfm-rag index --project cloudflare
Do not retry with a different project.
```

Recovery does not require a client restart: once the operator builds the index, the next call for that project loads it through the normal fingerprint reload. A call for a healthy project is never affected by another project's broken index. The `agent-knowledge` skill and `AGENTS.md` instruct the agent to report these errors verbatim to the operator and never to guess a substitute project.

### Project Healthcheck

```text
wfm-rag project healthcheck [--verify] [--purge] [--format text|json]
```

CLI-only maintenance command; never an MCP tool — MCP keeps exactly the two retrieval tools. Three sweeps, in order:

1. Registry liveness: every registered root must still be an existing directory. Findings: `root missing` per project. Repair: `project remove <id>` or re-`init` at the new location. healthcheck never edits the registry.
2. Index presence: every project's derived index path must be a regular file. Findings: `index missing`. `--verify` additionally runs the full `index.Load` per project (structural validation, root binding, size caps) and reports a corrupt snapshot as `index_invalid`.
3. Dangling indexes: every index directory under the state directory must be derivable from some registry entry via `index.DefaultPath`. Unclaimed directories are dangling — leftovers of removed projects or moved checkouts. `--purge` deletes a dangling directory only when it contains exactly one regular `index.json`; anything else (extra files, symlinks, unexpected entries) is reported for manual handling and never removed. Claimed directories are never touched, and nothing is ever written into target repositories.

Dangling detection is exact because derivation is deterministic: a directory is claimed if and only if some registered root hashes to it, so "does this index belong to a live project?" needs no bookkeeping.

The command is non-interactive: findings are data, not prompts. `project list` is the per-project view; healthcheck is the whole-state sweep. Exit 0 when everything is healthy; exit 1 when any finding exists, with findings on stdout (JSON mode: a findings array), so scripts and CI can branch on the exit code. A missing or invalid registry is itself a finding, matching `project_registry_missing` / `project_registry_invalid` elsewhere.

### Failure Behavior

| Condition | CLI | MCP tool result |
| --- | --- | --- |
| `--project` absent | exit 2, `project_required` | schema requires the field; a missing value is an input error |
| ID not configured for this process (call time) | exit 2, `project_unknown` listing configured IDs | `project_unknown` listing configured IDs |
| `mcp --project <id>` not in the registry (launch time) | exit 2 at startup, `project_unknown` listing registry IDs | not reachable; the server never starts |
| Malformed ID | exit 2, `project_id_invalid` | input validation error |
| More than 64 `--project` flags | exit 2, `project_limit_exceeded` | not reachable; the server never starts |
| Registry absent | exit 1, `project_registry_missing` | server refuses to start |
| Registry malformed, duplicate key, oversized, non-absolute stored root, unsafe file type | exit 1, `project_registry_invalid` | server refuses to start |
| Lock held by another writer | exit 1, `project_locked` | not reachable; MCP never writes the registry |
| Registered root no longer exists | exit 1, `project_root_missing` naming the project | tool error naming the project |
| Selected project's index missing or invalid | exit 1, `index_missing` naming the project and the repair command | tool error naming the project and the repair command; server keeps serving other projects |
| `project healthcheck` findings | exit 1, findings on stdout; repair via `project remove`, re-`init`, `index`, or `--purge` | not reachable; maintenance is CLI-only |

Every rejection is a complete answer. Nothing is served from a guessed, nearest, or merged corpus. Every MCP tool error names its project and states the operator's repair command, because the model is the party that will see it first.

### Snapshot And Restart Semantics

A running MCP process holds the project set and tool schemas built at launch. `project init` and `project remove` affect new CLI invocations immediately, and affect a running server only after the client restarts it. `project remove` is not revocation. Documentation and error text must say this rather than implying live control. Index content, by contrast, reloads live: a rebuilt index is picked up by the next call without a restart.

### Storage And Index Layout

Each project keeps its own root-bound index at:

```text
<stateDir>/<canonical-root-sha256>/index.json
```

where `<stateDir>` is `$XDG_DATA_HOME/wfm-rag` or the `~/.local/share/wfm-rag` fallback. The registry stores `id -> canonical root`, and `index.DefaultPath` derives the file. `index.Version` stays 1; no serialized field changes. A moved checkout changes the derived path, and `project init` in the new directory rebinds the ID.

Keying index directories by project ID instead of root hash was considered and rejected: it would migrate every existing index for no correctness gain, because index content is already bound to one canonical root.

### Search Semantics

Single-corpus retrieval returns. `search.Search` and `search.Get` operate on one index, and `SearchAll`, `GetAll`, the cross-root tie key, `IndexStatus`, `Response.Indexes`, per-result `Result.Root`, and `mcpserver.Collision` are deleted in the same change that migrates every caller and document. No compatibility shim is kept for an uncommitted design.

Search JSON becomes:

```json
{
  "project": "cloudflare",
  "root": "/canonical/root",
  "indexed_at": "2026-10-08T13:38:44Z",
  "query": "chrome incident",
  "results": [{ "id": "2026-09-29-429-slice", "score": 32, "...": "..." }]
}
```

Get JSON keeps its current `root`, `indexed_at`, and `card` fields and adds `project`. It never had `indexes[]` or per-result roots.

## Onboarding A Fresh Clone

```bash
# 1. install the binary once per machine
go build -o bin/wfm-rag ./cmd/wfm-rag

# 2. register the repository under an explicit ID
cd /path/to/Api
./bin/wfm-rag project init api

# 3. build that project's corpus
./bin/wfm-rag index --project api

# 4. one client entry, explicit allowlist
#    OpenCode: "command": "wfm-rag", "args": ["mcp", "--project", "api", "--project", "cloudflare"]
#    OMP:      "args": ["mcp", "--project", "api", "--project", "cloudflare"]

# 5. restart the client, then confirm what it can reach
./bin/wfm-rag project list
```

## Impact Checklist

| Area | Change |
| --- | --- |
| Card discovery, schema, status lifecycle, limits | none |
| Index default path resolution | behavior change: `index.DefaultPath` resolves `$XDG_DATA_HOME/wfm-rag` when set and absolute, else `~/.local/share/wfm-rag`; no fallback read, one re-`index` per project when `$XDG_DATA_HOME` is set |
| Serialized index fields, validation, `index.Version` | none; indexes stay root-bound at the derived path |
| Normalization, scoring weights, reason order, filters, excerpts | none; cross-root merge and tie key removed |
| New owner package | `internal/project`: registry read/write/validate, flock writer lock, ID rules, error sentinels |
| CLI commands, flags, JSON fields, error codes, exit status | new `project` subcommand group (`init`, `remove`, `list`, `healthcheck`); `--root` retired; `--project` required on `index`/`search`/`get`; `mcp` requires at least one `--project` or `--all-projects`, capped at 64; `search` JSON gains `project` and loses `indexes[]` and per-result `root`; `get` JSON gains `project` only; new error codes above |
| MCP tool names, schemas, structured output, reload, protocol | names unchanged; both tools gain required `project` with a launch-scoped enum; structured output returns to single-corpus shape; reload becomes per-selected-project; degraded startup with stderr warnings and actionable tool errors; server version `0.3.0` |
| Client configuration and documentation | one entry with an explicit `--project` allowlist; `docs/mcp-clients.md`, `docs/cli.md`, `docs/index-format.md`, `README.md`, `AGENTS.md`, and the `agent-knowledge` skill all state the project requirement, the state-directory rule, degraded-project error relay, and the restart rule |
| Containment, permissions, bounds, determinism | state directory `0700`; registry and lock file `0600` regular files, symlink rejection; 1 MiB registry cap; 64-project cap for registry and per MCP process; strict parsing with duplicate-key and non-absolute-root rejection; flock writer lock; sorted rendering; healthcheck `--purge` removes only single-regular-file dangling index directories and never follows symlinks; nothing is ever written into target repositories |

### Agent-Contract Lines

`AGENTS.md` currently states invariants this plan contradicts. The implementation must rewrite these exact lines, not just add a paragraph:

- "Default path is `~/.local/share/wfm-rag/...`; `$XDG_DATA_HOME` is not used." — replaced by the state-directory rule.
- "One MCP process serves the repository roots configured at launch, up to eight; `--root` is repeatable." — replaced by the `--project` allowlist, cap 64.
- "Card IDs must stay unique across those roots, and a colliding ID resolves to the first configured root." — deleted; IDs are unique within one corpus, and cross-corpus namespace sharing no longer exists.
- "With several configured roots, a failed reload for any of them fails the whole call." — replaced by per-selected-project reload: a failed reload fails only calls that select that project.
- Search tie order "score descending, root ascending, ..." — the root key is removed.
- Public Entry Points lists `SearchAll`, `GetAll`, `IndexStatus`, `Response.Indexes`, `Result.Root`, `mcpserver.Collision` — deleted; `internal/project` entry points added.

## Disposition Of Already-Written Code

Implemented 2026-10-08 under the previous plan, uncommitted:

- Keep: `mcpserver.New(ctx, sources)` shape and per-source fingerprint reload mechanics, retargeted to a project-keyed map with select-then-reload; the repeatable-flag plumbing in `cmd`, retargeted from `--root` to `--project`; multi-project subprocess test scaffolding.
- Replace: `search.SearchAll`/`search.GetAll` merging becomes a registry lookup that selects one index, then calls the unchanged single-index `search.Search`/`search.Get`.
- Remove: `search.IndexStatus`, `Response.Indexes`, `Result.Root`, `mcpserver.Collision` and its stderr reporting, cross-root tie ordering, and `--index` positional pairing.
- Add: `internal/project`, `runProject` dispatch in `cmd` (including the `project healthcheck` sweep), the state-directory resolver, and the new error codes.

## Tests

Existing multi-root tests that pin the removed API are deleted or rewritten in the same change, not left failing: `TestSearchAllMergesIndexesDeterministically`, `TestSearchAllWithOneIndexMatchesSearch`, `TestGetAllResolvesCollidingIDsInConfiguredOrder`, `TestSearchAllAndGetAllValidateIndexLists` (`internal/search/search_test.go`), `TestCrossRootCollisionsAreReportedAndResolvedInOrder` (`internal/mcpserver/server_test.go`), the collision and duplicate-root cases in `cmd/wfm-rag/main_test.go`, and the merged-index assertions in `cmd/wfm-rag/integration_test.go`.

`internal/index`

- `DefaultPath` resolves `$XDG_DATA_HOME/wfm-rag` when the variable is set and absolute; falls back to `~/.local/share/wfm-rag` when unset, empty, or relative.

`internal/project`

- Registry round-trip, `0600` mode, `0700` directory, symlink and non-regular-file rejection, oversized rejection, trailing-data rejection, duplicate-key rejection, unknown-field rejection, non-absolute stored-root rejection, sorted rendering, and preserved previous file after a failed write.
- Two processes racing `init` and `remove` under `flock` produce both updates; a writer that cannot take the lock fails with `project_locked`; a writer killed while holding the lock leaves no stale lock and the next writer succeeds.
- Slug acceptance and rejection; 64-entry cap.
- A structurally valid registry with a vanished root still supports `list`, `remove`, and re-`init`.

`cmd/wfm-rag`

- `--project` required on `index`, `search`, and `get`; missing and unknown cases exit 2 and list configured IDs.
- `mcp` with no `--project` exits 2; explicit values restrict the served set; a launch-time unknown project exits 2 listing registry IDs; more than 64 flags exits 2; `--all-projects` selects exactly the registry set.
- Registry absent, malformed, and unsafe-file-type cases exit 1 with stable JSON error codes.
- `project init` registers, rebinds a moved directory while reporting the previous root, and is a no-op refresh for the same directory.
- `project list` reports status without opening index files.
- `project healthcheck` exits 0 only when everything is healthy; reports dead roots, missing indexes, and dangling index directories; `--verify` reports a corrupt index as `index_invalid`; `--purge` removes only dangling directories containing a single regular `index.json` and preserves directories with extra or non-regular content; the command never writes the registry and never touches target repositories.

`internal/mcpserver`

- `tools/list` contains exactly two tools, both requiring `project`, with a launch-scoped sorted enum, no `root`/`path`/index/write/shell property, and `additionalProperties: false`.
- A call for one project cannot return cards from another configured project.
- Degraded startup: a configured project with a missing index starts the server with a stderr warning; a call for a healthy project succeeds; a call for the degraded project returns `index_missing` naming the project and the repair command; after the index is built, the next call succeeds without restarting the server.
- Unknown project returns `project_unknown` listing configured IDs and touches no index.

`cmd/wfm-rag` integration

- One subprocess, two configured projects: each call returns only its named project's cards.
- Structured and text output stay semantically equal.
- Fresh-clone flow (`project init`, `index`, MCP search) works end to end.

## Risks

1. Registry drift is new failure surface: moved checkout, deleted directory, hand-edited file. Mitigated by lock-protected writes, separate structural and liveness checks, rebinding on explicit `init`, stored-root validation, and `project list` as the inspection command.
2. The enum makes the served project set fixed for the lifetime of a process. Adding or removing a project requires a client restart. This is accepted, because clients cache tool schemas either way; index content itself reloads live.
3. Two CLI/JSON contract changes land close together: the 2026-10-08 merge plan introduced `indexes[]`, and this plan removes it. Cut over once, in one change, with no shim.
4. The XDG change orphans existing indexes when `$XDG_DATA_HOME` is set. Accepted: one re-`index` per project, no fallback read, and `project list` shows `index missing` until repaired.
5. A degraded project can sit unnoticed at launch until a call selects it. Mitigated by the stderr warning at startup and `project list` status; the model is instructed to report the error rather than work around it.
6. Cross-project questions cost separate calls. That is the intended price of isolation.

## Rollback

Revert the change. `projects.json` and the lock file are inert data that older builds ignore. One caveat: if `$XDG_DATA_HOME` was set and indexes were rebuilt under it, an older build reads only the legacy `~/.local/share` path, so rollback needs one re-`index` per project. Restoring the previous client configuration is a client-side edit.

## Review Record

1. Sol GPT 5.6, 2026-10-08: approve with changes. Adopted in rev 1: registry-only identity as the recommendation, launch-scoped required `--project` allowlist instead of default-all, enum generated from that allowlist, versioned registry wire format storing only canonical roots, strict parsing with duplicate-key detection, writer locking, separated structural and liveness validation, explicit moved-checkout rebinding, select-before-reload so an unrelated broken index cannot fail a call, separate MCP input envelopes, cheap `project list`, documented restart/snapshot semantics, accurate impact text for `get` JSON. Rejected: per-project registry files, a namespaced global repository identity, keeping one MCP server per corpus.
2. Parent assistant, 2026-10-09: approve with changes. Findings and dispositions:
   - Marker-mismatch row contradicted the exit-code invariant (exit 1 while the build proceeds). Resolved by dropping the marker entirely (operator decision).
   - Eager load at construction contradicted select-then-reload for broken indexes. Resolved: degraded startup with stderr warning, actionable per-project tool errors, live recovery without restart (operator decision).
   - Writer lock mechanism unspecified; an `O_CREATE|O_EXCL` lockfile leaves stale locks after a crash. Resolved: `flock(LOCK_EX | LOCK_NB)`, non-blocking failure with `project_locked`, crash-recovery test.
   - `--all-projects` cap 16 desynced from the 64-project registry. Resolved: 64 everywhere (operator decision).
   - `--index` override scope was ambiguous against `index.Load` root binding. Resolved: explicit subsection — `--index` relocates the file, never the expected root.
   - Launch-time unknown project was unspecified. Resolved: exit 2 at startup with `project_unknown` listing registry IDs.
   - Additional: stored roots validated as absolute at parse time; XDG state directory adopted for existing and new state (operator decision); AGENTS.md invariant lines enumerated for rewrite; the existing multi-root tests that must be deleted are enumerated.
3. Operator additions, 2026-10-09: `project healthcheck` added as a CLI-only maintenance sweep (registry liveness, index presence with optional `--verify`, dangling-index detection with deletion only under explicit `--purge`; non-interactive findings, exit 0 healthy / 1 findings). `--dir` scope tightened to `project init` only, with the rationale that a build-time directory flag would create a second corpus locator and produce an index its own project ID rejects. Keying indexes by project ID was re-evaluated and rejected again: the path-derived directory is the mechanism that makes a snapshot unreachable once its corpus moves or is rebound, which is the property that prevents silently serving a stale corpus.
4. Operator decision, 2026-10-09 (post-implementation): `project init` auto-scaffolds the KB skeleton. Registration alone left new corpora unusable until an agent manually copied schema, templates, and a skill from the tool repository. `init` now creates the missing skeleton in the target repository: seven category directories, `schema.md`, eight card templates, and a starter agent-knowledge skill with the project ID rendered in. The invariant is narrowed, not dropped: scaffolding is create-only (existing files, directories, and symlinks are never overwritten or written through), it is the only target-repository write in the tool, and no identity, registry, or retrieval state ever lands in a target repository. A wrong-kind skeleton path fails with `project_scaffold_failed` after the registration already stands. Implemented in `internal/scaffold`.
5. Operator decision, 2026-10-18: the scaffold gained `.agents/knowledge/README.md`, a documentation file (never a card candidate) carrying the prompt that fills the starter skill's `Repository Profile`, and `project init` text output prints that next step only alongside a scaffolded-file list while the starter skill still carries the profile placeholders. Skeleton creation was anchored to an opened root descriptor (`os.OpenRoot`) with per-component symlink rejection and exclusive `O_CREATE|O_EXCL` file creation, so no scaffold write can leave the target repository or traverse an observed symlink. Item 4's file list is kept as the historical record; the current list is owned by `AGENTS.md` and `docs/cli.md`.
