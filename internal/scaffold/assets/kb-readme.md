# Knowledge Base

This repository's knowledge base is registered as `{{PROJECT}}` in the local
`wfm-rag` registry. Cards are Markdown under `.agents/knowledge/`; the JSON
index is derived state, rebuilt with `wfm-rag index --project {{PROJECT}}`.

- Card schema and frontmatter fields: `.agents/knowledge/schema.md`
- Retrieval and card-lifecycle rules: `.agents/skills/agent-knowledge/SKILL.md`
- Card templates to copy: `.agents/knowledge/templates/`

This file is documentation, not a card: files directly under
`.agents/knowledge/` and any file named `README.md` are never indexed.

## First Task For The Agent

The generated starter skill has a placeholder `Repository Profile`. Hand this
prompt to the agent that will work in this repository, once, before its first
real task:

```text
Read .agents/skills/agent-knowledge/SKILL.md and fill its "Repository Profile"
section with this repository's real values, inventing nothing:

- Project ID, root, binary, index path, and MCP server name: verify each
  against `wfm-rag project list` and the client's MCP configuration.
- High-value retrieval triggers: the domain nouns, package and module names,
  tool, command, and file names, and error strings an agent would actually
  type here. Take them from this repository's own code, README, and AGENTS.md.
- Required evidence and verification commands: the exact commands that prove a
  claim here (test, build, lint, or service commands).

Keep every generic section of the skill unchanged; repository-specific content
belongs only in the profile. Then run `wfm-rag index --project {{PROJECT}}` and
report the profile you wrote for review.
```

A profile that still contains `<project-specific terms>` or
`<project-specific commands>` is unfinished. Retrieval is lexical with no
synonym expansion, so those triggers decide what a future agent finds here: an
unfilled profile makes the knowledge base much harder to use.
