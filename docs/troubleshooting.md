# Troubleshooting

## Quick Diagnostic Sequence

Use the same absolute root and index path configured in the client:

```bash
binary=/absolute/path/to/wfm-rag
repo=/absolute/path/to/repository
index=/absolute/path/to/configured-index.json

"$binary" index --root "$repo" --index "$index" --format json
"$binary" search --root "$repo" --index "$index" "known query"
"$binary" get --root "$repo" --index "$index" known-card-id
```

If the client omits `--index`, omit it consistently from these commands to test the default root-derived path.

If these pass, investigate client configuration and captured stderr. If they fail, fix the CLI/index problem first.

## Symptoms

| Symptom | Likely cause | Action |
| --- | --- | --- |
| `index missing` | Repository was not indexed, different OS user, or different `--index` | Reindex with the exact root/user/path used by the consumer |
| `index belongs to another root` | One custom index was reused, or checkout moved | Rebuild for the configured canonical root |
| `invalid index` | Malformed, oversized, unsafe, partial, or structurally inconsistent JSON | Re-run `index`; do not hand-edit JSON |
| `unsupported index version` | Binary and index format differ | Rebuild with the current binary |
| Card edit is invisible | Markdown changed without reindex | Run `index` successfully |
| Retired/deleted card remains visible | Existing snapshot was not replaced | Reindex and check warnings/errors |
| New active card is absent | Wrong category/name, invalid status, or build failed | Check discovery rules and index output |
| Duplicate ID after retirement | Inactive IDs remain reserved | Keep one source card per ID |
| `get` reports not found | Wrong case, inactive card, stale index, or wrong root | Use exact ID and inspect active index documents |
| Search returns no results | No lexical token match or filters exclude the card | Simplify query, check status, metadata, and filters |
| Search has irrelevant tail results | One common term scored a chunk | Use stronger terms/filters; lexical search has no stop-word removal |
| Path filter excludes expected card | Filter compares frontmatter citations | Inspect `paths`; it does not compare the card source filename |
| MCP process appears hung | It is waiting for stdio JSON-RPC | Let an MCP client launch/manage it |
| MCP client shows no tools | Executable/index/config/startup error | Use absolute binary path, run CLI checks, inspect stderr/client status |
| MCP reload returns error | Replacement is missing, malformed, unstable, or root-mismatched | Build a valid atomic index; stale memory is intentionally not served |
| Client connects to wrong repository | Global/client config shadows project entry | Use unique per-repository server names and absolute roots |

## Index Path Confusion

The default index path depends on the canonical root and current OS user's home:

```text
~/.local/share/wfm-rag/<canonical-root-sha256>/index.json
```

Consequences:

- different users have different defaults;
- moving a checkout changes the default;
- root symlink aliases resolve to the same canonical target;
- `$XDG_DATA_HOME` does not change this path;
- a relative explicit `--index` is resolved from the process working directory.

Prefer absolute custom paths in MCP client configuration.

## Corpus Discovery Problems

A source card is considered only when all are true:

- `.agents` and `.agents/knowledge` are real directories;
- first category is one of the five allowed names;
- file is regular, not a symlink;
- extension is lowercase `.md`;
- basename is not `README.md`.

Draft and retired cards are intentionally absent from the index but generate warnings.

## Search Expectations

Search lowercases and tokenizes letters/numbers. It does not stem or understand synonyms:

```text
parse != parser != parsing
config != configuration
```

Use triggers for likely variants or query with exact repository terminology.

Path matching direction matters. A citation `src/foo` matches filter `src/foo/file.go`, but not broader filter `src`. A citation does not authorize source reads.

## MCP Stdout Contamination

The MCP process must write only protocol JSON to stdout. Do not launch it through wrappers that print banners, shell tracing, progress, or diagnostics to stdout. Send wrapper diagnostics to stderr.

Use an installed binary instead of `go run` for stable startup.

## OpenCode

```bash
opencode mcp list
```

After manually changing OpenCode configuration, quit and restart OpenCode. It loads configuration at startup.

See `docs/mcp-clients.md` for the exact current schema.

## OMP

Launch OMP from the repository root or use `omp --cwd /absolute/repository/root`. OMP 17.1.5 does not search ancestors for MCP project files.

After manually editing an MCP file, restart OMP 17.1.5, then run:

```text
/mcp list
/mcp test agent-knowledge
```

Global OpenCode config can shadow same-named project OpenCode config when imported by OMP. Use unique per-repository names or an OMP-native project entry.

## Recovery Guarantees

- A malformed source corpus does not replace the previous valid index.
- A temporary write failure does not replace the previous valid index.
- MCP does not serve a stale snapshot after detecting a broken replacement.
- Rebuilding a valid index allows a running MCP process to recover on the next valid search/get.
