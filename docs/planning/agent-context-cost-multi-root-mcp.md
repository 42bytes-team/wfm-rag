# Agent Context Cost And Multi-Root MCP: 2026-10-08

> **Superseded 2026-10-09 by [`project-scoped-knowledgebase.md`](project-scoped-knowledgebase.md).** The token measurements and the motivation for one multi-corpus process remain valid history. The merge semantics implemented from this plan — cross-root `SearchAll` merging, `Response.Indexes`, per-result `Result.Root`, `mcpserver.Collision`, repeatable `--root` with positional `--index` pairing, and the 8-root cap — were removed the same day they landed. Retrieval is now project-scoped: the registry maps project IDs to canonical roots, every call names exactly one project, and one MCP process serves a launch-time `--project` allowlist. Read this file only for the measurements; do not implement from it.

## Problem

The operator configuration runs one `wfm-rag mcp` process per corpus: `Api`, `wf-unpacker`, `Manager`, and `Cloudflare`. Each process publishes its own pair of tools into the agent client, so the same two tool definitions are repeated four times in every model request. Retrieval quality pays for that repetition twice: an agent searching the `Api` server cannot see `Cloudflare` cards, so a cross-repository question depends on the agent guessing which server name to call.

The v0.1 contract states "one process serves one fixed root" (`AGENTS.md`, `docs/mcp-clients.md`). That section is amended by this plan.

## Measurement Method

Every figure is the `input` token count of the first provider request in a headless OMP session:

```bash
omp -p --mode json --no-session --no-title "reply with exactly: ok" \
  | jq -r 'select(.type=="agent_end") | .messages[-1].usage.input'
```

Measurements used the operator's local model `strata/qwen3.8-flash-next-iq3_xxs` (192,608-token window, `maxTokens: 24576`). An isolated OMP profile (`--profile tmp-measure`) held skills, instruction files, memory backend, and tool selection constant while one variable changed at a time; per-tool settings were applied through `--config` overlays. The throwaway profile was deleted after measurement.

## Measured Cost

| Configuration | First-request tokens | Delta |
| --- | ---: | ---: |
| No MCP servers, one tool enabled | 3,706 | baseline |
| Four `wfm-rag` knowledge servers added | 4,534 | +828 |
| `chrome-devtools-mcp` added instead | 5,635 | +1,929 |
| All tools, isolated profile | 20,226 | — |
| All tools, operator live profile | 22,932 | — |

Each `wfm-rag` server costs about 207 tokens per request, and the request repeats for every turn of the session. Four servers cost 828 tokens per turn for corpora that the agent must address by server name.

Two related measurements explain why the repetition is not fixable on the client side alone:

- Client-side tool allow-lists do not filter MCP tools. OMP `--tools read` still carried all four knowledge servers.
- Approval policy is not a context control. OMP `tools.approval` set to `deny` for three tools measured 20,226 tokens, identical to baseline.

## Decision

Serve several repository indexes from one `wfm-rag mcp` process. Keep exactly two tools, keep their input schemas closed-world, and make root attribution output-only. `search` and `get` accept the same repeatable `--root` so a human can reproduce any MCP result set through the CLI.

This is an explicit requirement, not speculative generality: it is the change that removes three of four MCP server definitions from the operator configuration.

## Design

### Command surface

```text
wfm-rag mcp    --root <repo> [--root <repo> ...] [--index <file> ...]
wfm-rag search --root <repo> [--root <repo> ...] ... <query>
wfm-rag get    --root <repo> [--root <repo> ...] <card-id>
wfm-rag index  --root <repo> [--index <file>]
```

- `--root` is repeatable for `mcp`, `search`, and `get`. At least one is required; canonical duplicates collapse in first-seen order.
- `--index` is repeatable and pairs with `--root` by order. It must be supplied either zero times, so every root uses its own default root-derived path, or exactly once per root. Any other count exits 2 with `invalid_arguments`.
- `index` stays single-root: a corpus build is one canonical root by definition.
- A hard bound of 8 configured roots keeps loaded index bytes bounded; exceeding it exits 2.

### `internal/search`

Retrieval semantics stay in `internal/search`; no adapter implements merging.

```go
type IndexStatus struct {
    Root      string `json:"root"`
    IndexedAt string `json:"indexed_at"`
}

type Response struct {
    Query   string        `json:"query"`
    Indexes []IndexStatus `json:"indexes"`
    Results []Result      `json:"results"`
}

type Result struct {
    // existing fields
    Root string `json:"root"`
}

func SearchAll(values []*indexpkg.Index, query Query) (Response, error)
func GetAll(values []*indexpkg.Index, id string) (GetResponse, error)
```

- `Search` becomes `SearchAll([]*indexpkg.Index{value}, query)` and `Get` becomes `GetAll([]*indexpkg.Index{value}, id)`, so both adapters share one implementation.
- Per-index scoring, filters, best-chunk-per-card dedup, and reason order are unchanged.
- Cross-root ordering: score descending, then root ascending, then source ascending, then ordinal ascending. With one root the root key is constant, so single-root ordering is byte-identical to v0.1.
- `limit` applies once to the merged list, not per index.
- `GetAll` returns the first match in configured root order. A card ID present in several roots is a configuration collision, not a search failure.
- `GetResponse` is unchanged: its `root` names the index that matched.

### `internal/mcpserver`

```go
type Source struct {
    Root      string
    IndexPath string
}

type Collision struct {
    CardID string
    Roots  []string
}

func New(sources []Source) (*mcp.Server, []Collision, error)
func Run(ctx context.Context, sources []Source, transport mcp.Transport) error
```

- Tool names stay `agent_knowledge_search` and `agent_knowledge_get`. Search input stays `search.Query`; get input stays `GetInput`. Neither accepts a root, project, index path, write, or shell field.
- Search description changes from "the server's fixed repository index" to "the configured repository indexes".
- Each source keeps its own path, value, and fingerprint under one mutex. Before a valid call, every source is stat-compared; a changed source reloads through root-bound `index.Load` with the existing before/after fingerprint check. Any failed reload returns `ErrReload` for the whole call; the server never serves a partially refreshed state.
- `New` reports card IDs that appear in more than one configured index. `cmd` prints those to stderr. `get` resolves them by configured order.

### `cmd/wfm-rag`

- `commonFlags` holds `--root` and `--index` as repeatable values; `index` still requires exactly one root.
- Text output stays identical for a single index. With more than one index, the header lists each index and every result carries a `Root:` line.
- JSON output changes shape for `search`: `root` and `indexed_at` are replaced by `indexes[]`, and each result gains `root`. `get` JSON is unchanged.

## Impact Checklist

| Area | Change |
| --- | --- |
| Card discovery, schema, status lifecycle, limits | none |
| Serialized index fields, validation, `index.Version` | none; indexes stay root-bound and `Version` remains 1 |
| Normalization, scoring weights, reason order, filters, excerpts | none; only the cross-root tie key is added |
| CLI flags, JSON fields, error codes, streams, exit status | `--root`/`--index` repeatable; `search` JSON `indexes[]` and `result.root`; new `invalid_arguments` case for `--index` count mismatch and for more than 8 roots |
| MCP tool names, schemas, structured output, reload, protocol | names and inputs unchanged; structured output adds `indexes[]` and `result.root`; descriptions reworded; reload becomes per-source; server reports implementation version `0.2.0` |
| Client configuration and documentation | one server entry replaces several; `docs/mcp-clients.md` "Multiple Repositories" rewritten |
| Containment, permissions, bounds, determinism | new bound of 8 configured roots; no new filesystem access; deterministic merge order and first-seen root dedup |

## Tests

`internal/search/search_test.go`

- Existing single-index assertions updated to `Indexes` and `Result.Root`, with ordering assertions unchanged.
- `SearchAll` with two indexes reproduces single-index ordering when only one index matches.
- Cross-root merge asserts the full ordering: score, then root, then source, then ordinal.
- Merged `limit` truncates across roots rather than per root.
- The same card ID in two roots yields two results, each carrying its own root.
- `GetAll` resolves a colliding ID to the first configured index and reports that index's root.
- Empty slice or nil entry returns `ErrInvalidArguments`.

`internal/mcpserver/server_test.go`

- `tools/list` still contains exactly two tools for a multi-source server.
- Search across two sources returns cards from both with correct roots.
- Get resolves a colliding ID by configured order.
- `New` reports a cross-root collision and starts normally.
- A changed second index reloads independently; a broken second index returns `ErrReload` and the call does not serve stale data.

`cmd/wfm-rag/main_test.go`

- Repeatable `--root` on `mcp`, `search`, and `get`.
- `--index` count mismatch exits 2 with `invalid_arguments`.
- More than 8 roots exits 2.
- Duplicate canonical roots collapse to one search.
- Single-index text and JSON output match the v0.1 shape.

`cmd/wfm-rag/integration_test.go`

- One subprocess with two roots: `tools/list` has exactly two tools, search returns cards from both corpora, structured and text output remain semantically equal, stdout stays protocol-only, and stdin EOF still shuts the process down.

## Risks

1. A large corpus can crowd a smaller one out of the merged result limit, because the limit now spans roots. Root attribution makes that visible, and citation filters remain available. If it bites in real use, the fix is per-root result quotas or a roots filter, and both need measured evidence first.
2. Cross-root card ID collisions silently shadow one card in `get`. Configured-order resolution plus a startup stderr warning makes the behavior deterministic and visible rather than accidental.
3. One process now holds several indexes, so a broken index for one corpus fails calls that previously succeeded. That is intentional: serving stale data for one corpus while claiming to serve all of them is worse. Reindexing recovers without a restart.

## Rollback

Revert the change and restore one named server entry per corpus in the client configuration. Existing indexes remain valid and no index rebuild is required.

## Open Decisions

- `index` accepting several roots is rejected. A build is one canonical corpus, and per-root failure isolation is useful.
- A `roots` input filter on `agent_knowledge_search` is rejected for now. It would widen the tool input surface, which the closed-world invariant forbids, and the measured problem is server count rather than root selection.

## Executed Verification

The operator configuration replaced four knowledge servers with one `agent-knowledge` entry holding four roots. First-request input tokens in OMP:

| Configuration | Before | After | Saving |
| --- | ---: | ---: | ---: |
| All tools | 22,932 | 22,571 | 361 |
| `--tools read` | 8,003 | 7,615 | 388 |

The saving is the removal of six MCP tool routes. It is real but small next to the built-in tool surface, which measured 15,000 tokens in the same session. Client-side knobs are the larger lever: `tools.xdev: false` costs +7,580, `tools.intentTracing: false` saves 1,567, and `chrome-devtools-mcp` alone costs 1,929.

Two findings from the operator's own corpora:

1. Five card IDs collide across the configured roots: `agent-working-conventions` (`Api`, `wf-unpacker`), `project-directory-reference`, `project-purpose`, and `wfm-database` (`wf-unpacker`, `Manager`), and `project-state` (`wf-unpacker`, `Cloudflare`). The server reports all five on stderr at startup, and `agent_knowledge_get` resolves each to the first configured root. Renaming the duplicated profile cards in the target corpora is required before collisions can be treated as rare; that is corpus authoring work in the target repositories, not a change in this tool.
2. Merged limits crowd out weaker corpora, as predicted in Risk 1. Query `config` with `--limit 6` returned one `Api` card and five `Cloudflare` cards; query `rate limit` with `limit 8` returned eight `Cloudflare` cards. Root attribution makes the crowding visible, and citation filters remain the mitigation. Per-root quotas stay out until repeated dogfood evidence justifies them.
