---
name: git-workflow
description: Use for Git status, staging, commits, commit messages, branch operations, or repository setup in wfm-rag.
---

# Git Workflow

- Do not commit unless explicitly requested.
- Do not amend, rebase, reset, checkout, clean, stash, or force-push unless explicitly requested.
- Before a commit, inspect `git status --short`, `git diff`, and recent log; stage only intended files.
- Use conventional commit messages with concise imperative subjects.
- Report the commit hash and verification after committing.
- Keep target-repository knowledge changes and wfm-rag implementation changes in their respective repositories.
- Before staging, exclude generated/default/custom indexes, temporary indexes, binaries, and unrelated knowledge-card changes in target repositories.
- Run verification from the repository root and report the resulting commit hash.
