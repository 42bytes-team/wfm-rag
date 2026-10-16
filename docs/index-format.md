# Index Format

`wfm-rag` stores disposable retrieval state as a versioned JSON document. Markdown beneath the repository's `.agents/knowledge/` directory remains the source of truth.

## Location

The state directory is `$XDG_DATA_HOME/wfm-rag` when `$XDG_DATA_HOME` is set and absolute; a relative or empty value is invalid per the XDG Base Directory spec and falls back to `$HOME/.local/share/wfm-rag`. The same directory holds the project registry (`projects.json` and its `projects.lock`).

The default index path is:

```text
<state-dir>/<root-sha256>/index.json
```

`root-sha256` is the full lowercase SHA-256 digest of the canonical absolute repository root.

The root is made absolute, has root symlinks resolved, and is cleaned before hashing. Different symlink aliases for one target share the same default index. Moving the checkout changes the canonical root and therefore the path; `project init` in the new directory rebinds the project ID and the index must be rebuilt.

Indexing and retrieval should run as the same OS user, because the state directory is user-owned `0700` data.

An explicit `--index` path overrides only which file is read or written; the index remains bound to the selected project's canonical root and a mismatched file is rejected. Use one identical absolute custom path for `index`, `search`, and `get`. `mcp` does not accept `--index`.

## Version 1

```json
{
  "version": 1,
  "root": "/canonical/repository/root",
  "generated_at": "2026-08-12T12:00:00Z",
  "documents": [],
  "chunks": []
}
```

`documents` contains one complete active card with its metadata, title, repository-relative source path, and original Markdown content. `chunks` contains the bounded heading sections used by search, including card metadata, source lines, and deterministic ordinals.

The index therefore contains complete card Markdown, not only excerpts. Treat file permissions as meaningful and do not commit generated indexes.

The loader rejects unknown versions, malformed JSON, extra JSON values after the object, indexes for another canonical root, symlinks, non-regular files, and files larger than 512 MiB. It also rejects a structurally inconsistent snapshot: a missing or non-clean-absolute `root`, a zero `generated_at`, null `documents` or `chunks`, a document with an empty ID/title/source/content, an unsupported type, a non-active document, an invalid source path, a duplicate document ID or source, a chunk referencing an unknown card or missing its fields, an invalid chunk line range, a chunk whose type/title/source/scope/trigger/package/path values disagree with its document, a duplicate chunk ordinal, a non-contiguous ordinal sequence, or a document with no chunks.

## Replacement

Every index operation rebuilds the complete value in memory. It writes a `0600` temporary sibling, syncs and closes it, then renames it over the destination. Newly created index directories use `0700`. A failed corpus build or temporary write leaves the previous index untouched.

Existing parent directories are not chmodded. The `0700` mode applies only to directories created by `wfm-rag`.
