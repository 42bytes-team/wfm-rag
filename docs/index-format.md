# Index Format

`wfm-rag` stores disposable retrieval state as a versioned JSON document. Markdown beneath the repository's `.agents/knowledge/` directory remains the source of truth.

## Location

The default path is:

```text
~/.local/share/wfm-rag/<root-sha256>/index.json
```

`root-sha256` is the full lowercase SHA-256 digest of the canonical absolute repository root.

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

The loader rejects unknown versions, malformed structures, indexes for another canonical root, symlinks, non-regular files, and files larger than 512 MiB.

## Replacement

Every index operation rebuilds the complete value in memory. It writes a `0600` temporary sibling, syncs and closes it, then renames it over the destination. Newly created index directories use `0700`. A failed corpus build or temporary write leaves the previous index untouched.
