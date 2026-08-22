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
- Keep API repository changes and wfm-rag repository changes in their respective repositories.
