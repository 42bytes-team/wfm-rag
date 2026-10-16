# Troubleshooting

## Quick Diagnostic Sequence

Use the same project ID and index path configured in the client:

```bash
binary=/absolute/path/to/wfm-rag

"$binary" project list
"$binary" index --project api --format json
"$binary" search --project api "known query"
"$binary" get --project api known-card-id
```

Add `--index /absolute/path/to/configured-index.json` to each command only when the client configures a non-default index path; omit it to test the default root-derived path. `--index` relocates the file only: the expected root always stays the selected project's registered root.

If these pass, investigate client configuration and captured stderr. If they fail, fix the CLI/index problem first.

## Symptoms

| Symptom | Likely cause | Action |
| --- | --- | --- |
| `project_required` (exit 2) | Command omitted `--project` | Add `--project <id>`; `wfm-rag project list` prints the registered IDs |
| `project_unknown` (exit 2) | ID is not registered, or is not in the MCP process's launch `--project` allowlist | CLI: `wfm-rag project init <id> --dir <path>`; MCP: restart the client with the project in its launch arguments |
| `project_id_invalid` (exit 2) | ID does not match `[a-z0-9][a-z0-9-]{0,31}` | Use a lowercase slug |
| `project_limit_exceeded` (exit 2) | More than 64 `--project` flags on `mcp` | Serve fewer projects |
| `project_root_missing` (exit 1) | Registered root no longer exists | `project remove <id>`, or re-`init` at the new path |
| `project_locked` (exit 1) | Another `project` command holds `<stateDir>/projects.lock` | Retry once the other writer exits; a crashed writer leaves no stale lock |
| `project_registry_missing` / `project_registry_invalid` (exit 1) | Registry absent, hand-edited, oversized, duplicate keys, unknown fields, or non-absolute stored root | Re-run `project init`, or restore `<stateDir>/projects.json` from a known-good copy |
| `project_scaffold_failed` (exit 1) | A skeleton path in the target repository exists but is not the expected kind (a file where a directory belongs, or a symlink) | Fix that path and re-run `project init`; the registration itself already stands |
| `index missing` (exit 1) | Project was never indexed, different OS user, different `--index`, or a moved checkout | Run `wfm-rag index --project <id>`; confirm with `project list` or `project healthcheck` |
| `index belongs to another root` | Index file was built for a different canonical root (copied file, moved checkout) | Rebuild at the registered root: `wfm-rag index --project <id>` |
| `invalid index` | Malformed, oversized, unsafe, partial, or structurally inconsistent JSON | Re-run `index --project <id>`; do not hand-edit JSON |
| `unsupported index version` | Binary and index format differ | Rebuild with the current binary |
| Card edit is invisible | Markdown changed without reindex | Run `index --project <id>` successfully |
| Retired/deleted card remains visible | Existing snapshot was not replaced | Reindex and check warnings/errors |
| New active card is absent | Wrong category/name, invalid status, or build failed | Check discovery rules and index output |
| Duplicate ID after retirement | Inactive IDs remain reserved within one project | Keep one source card per ID per project |
| `get` reports not found | Wrong case, inactive card, stale index, or wrong project | Use exact ID and inspect active index documents |
| Search returns no results | No lexical token match or filters exclude the card | Simplify query, check status, metadata, and filters |
| Search has irrelevant tail results | One common term scored a chunk | Use stronger terms/filters; lexical search has no stop-word removal |
| Path filter excludes expected card | Filter compares frontmatter citations | Inspect `paths`; it does not compare the card source filename |
| MCP process appears hung | It is waiting for stdio JSON-RPC | Let an MCP client launch/manage it |
| MCP client shows no tools | Executable/registry/index/config/startup error | Use absolute binary path, run `project list` and the CLI checks above, inspect stderr/client status |
| MCP call fails for one project only | That project's index is missing or invalid; the other configured projects keep serving | Run the repair command named in the tool error; the next call recovers without a client restart |
| MCP reload returns error | Replacement index is missing, malformed, oversized, unstable, or built for another root | Build a valid atomic index for that project; stale memory is intentionally not served |
| Client selects the wrong project | Global/client config shadows the project entry, or the launch allowlist names the wrong IDs | Launch with an explicit `--project` allowlist, restart the client, and confirm with `project list` |

## Index Path Confusion

The default index path depends on the canonical root and the state directory:

```text
<stateDir>/<canonical-root-sha256>/index.json

<stateDir> = $XDG_DATA_HOME/wfm-rag        when $XDG_DATA_HOME is set and absolute
           = $HOME/.local/share/wfm-rag    otherwise
```

Consequences:

- different users have different defaults;
- an unset, empty, or relative `$XDG_DATA_HOME` falls back to `$HOME/.local/share/wfm-rag`;
- moving a checkout changes the derived path, so `project list` reports `index missing` until `project init` rebinds the ID and `index` rebuilds;
- root symlink aliases resolve to the same canonical target;
- a relative explicit `--index` is resolved from the process working directory.

Prefer absolute custom paths in MCP client configuration. `--index` relocates the index file only; the expected root stays the selected project's registered root.

## Corpus Discovery Problems

A source card is considered only when all are true:

- `.agents` and `.agents/knowledge` are real directories;
- first category is one of the seven allowed names: `anti-patterns`, `dependencies`, `incidents`, `patterns`, `research`, `sources`, `state`;
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

The list prints each configured server with its status (`connected`, `pending`, `disabled`). A server that never appears is usually a shape problem: V2 reads servers from `mcp.servers`, and an entry under a key OpenCode does not read contributes nothing. `opencode debug config` shows the resolved configuration, including normalized legacy entries.

After manually changing OpenCode configuration, quit and restart OpenCode. It loads configuration at startup.

See `docs/mcp-clients.md` for the exact current schema.

## OMP

Project MCP configuration is `.omp/mcp.json`, keyed to the working directory, so launch OMP from the repository root or use `omp --cwd /absolute/repository/root`. User-level configuration is `~/.omp/agent/mcp.json`, or `~/.omp/profiles/<name>/agent/mcp.json` for a named profile.

Inside a session:

```text
/mcp list
/mcp test agent-knowledge
/mcp reconnect agent-knowledge
```

`/mcp list` also shows which file a server came from. A same-named definition in a higher-priority source wins and the lower one is silently replaced, so an imported global OpenCode entry can bind OMP to a different `--project` allowlist than the project entry you edited. Use unique per-repository names such as `api-agent-knowledge`, or define the server in `.omp/mcp.json`, which outranks every imported OpenCode entry. `disabledServers` in the active profile's user file hides a discovered server by name regardless of its source.

`/mcp reload` rediscovers after OMP-managed changes. After an edit made by another editor, restart OMP when `/mcp list` still reports stale contents.

## Recovery Guarantees

- A malformed source corpus does not replace the previous valid index.
- A temporary write failure does not replace the previous valid index.
- MCP does not serve a stale snapshot after detecting a broken replacement.
- Rebuilding a valid index allows a running MCP process to recover on the next valid search/get.
- A `project` command that cannot take the writer lock leaves the previous registry untouched.
- `project remove` deletes only the registry entry; repository files and index files are never deleted by it.
