# API Retrieval Dogfood: 2026-08-22

## Scope

Step 10 evaluated the live `../Api` knowledge corpus without changing search scoring or card content.

The corpus indexed successfully into an isolated temporary index:

- 3 active cards
- 18 chunks
- 4,702 source bytes
- 0 skipped cards
- 0 warnings

CLI and stdio MCP calls used the same generated index.

## Retrieval Results

| Query or operation | Expected | Observed | Result |
| --- | --- | --- | --- |
| `config parser comma separated` | `ini-list-parsing` first | `ini-list-parsing` first at 22; `reuse-existing-capabilities` also scored 22 | Pass, tied score |
| `pointer helper` | `modern-go` first | `modern-go` at 22, then `reuse-existing-capabilities` at 20 | Pass |
| `before writing a custom parser utility` | `reuse-existing-capabilities` first | First at 23; next result scored 2 | Pass |
| `slices contains collection check` | `modern-go` first | First at 23; two unrelated tail results scored 1 | Pass with tail noise |
| `Key.Strings INI` | `ini-list-parsing` first | Only result, score 26 | Pass |
| `avoid reinventing utility` | `reuse-existing-capabilities` first | First at 24; two generic `Avoid` heading matches scored 4 | Pass |
| `parse a list from configuration` | `ini-list-parsing` first | First at 5; two unrelated tail results scored 1 | Weak pass |
| `create a bool pointer` | `modern-go` first | First at 21; two unrelated tail results scored 1 | Pass |
| `find existing dependency method before coding` | `reuse-existing-capabilities` first | First at 6; two unrelated tail results scored 1 | Weak pass |
| `quantum banana websocket` | No results | No results | Pass |
| Path filter `src/pkgs/config/ini.go` with `config` | Only `ini-list-parsing` | One result, score 61 | Pass |
| Path filter `src/apps/vex-web/handlers/user/request.go` with `pointer` | Only `modern-go` | One result, score 61 | Pass |
| Package filter `gopkg.in/ini.v1` with `parser` | Only `ini-list-parsing` | One result, score 51 | Pass |
| Type filter `dependency` with `helper` | No results | No results | Pass |
| Get all three stable IDs | Complete original cards | Metadata, Markdown, source, and citations preserved | Pass |

## MCP Results

The real `wfm-rag mcp` stdio process was exercised against the API index with a client that waited for each response before closing stdin.

- `server/discover` negotiated protocol `2026-07-28`.
- `tools/list` returned only `agent_knowledge_get` and `agent_knowledge_search`.
- Search returned the same `ini-list-parsing`, `reuse-existing-capabilities` ordering as CLI.
- Get preserved `Key.Strings(",")`, the source card path, and both citations.
- Every stdout line decoded as JSON protocol output.
- Stderr remained empty.
- The process exited successfully after stdin EOF.

## Recorded Gaps

No top-result miss, missing card, filter failure, CLI/MCP mismatch, or protocol failure occurred.

Three non-blocking retrieval weaknesses were observed:

1. The acceptance query `config parser comma separated` gives the specific INI card and the generic reuse card the same score. Deterministic source ordering currently puts the intended card first, but the zero score margin is fragile.
2. Queries containing common terms such as `a`, `before`, `avoid`, `contains`, or `collection` can return unrelated one-point tail results. This does not affect the current top result but could consume result slots in a larger corpus.
3. Natural variants such as `parse` versus `parser` or `parsing`, and `configuration` versus `config`, receive no normalization beyond tokenization. `parse a list from configuration` still finds the INI card through `list`, but only with score 5.

## Decision

Do not change ranking for v0.1 based on this three-card corpus. The required top results are correct and deterministic, while stop-word filtering, stemming, score thresholds, or score-formula changes would be broader than the measured failures justify.

Revisit these recorded gaps when a larger corpus causes a top-result miss, a relevant card to fall outside the result limit, or an agent evaluation to fail.
