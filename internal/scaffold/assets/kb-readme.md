# Knowledge Base

This repository's knowledge base is registered as `{{PROJECT}}` in the local
`wfm-rag` registry. Cards are Markdown under `.agents/knowledge/`; the JSON
index is derived state, rebuilt with `wfm-rag index --project {{PROJECT}}`.

- Card schema and frontmatter fields: `.agents/knowledge/schema.md`
- Retrieval and card-lifecycle rules: `.agents/skills/agent-knowledge/SKILL.md`
- Card templates to copy: `.agents/knowledge/templates/`

This file is documentation, not a card: files directly under
`.agents/knowledge/` and any file named `README.md` are never indexed.

## Wire It Into AGENTS.md

Scaffolding never edits `AGENTS.md`, and a skill nothing routes to is a skill no
agent loads. Add three things to this repository's `AGENTS.md`.

A gate above the skill routing table, so knowledge is consulted before the agent
starts guessing:

```markdown
**Universal gate:** Load `.agents/skills/agent-knowledge/SKILL.md` and complete
its KB preflight **before any repository read, command, plan, delegation, or
conclusion**. This is not conditional task routing. After that preflight, load
the other matching skills below.
```

A routing row that matches how work is actually requested here:

```markdown
| Project convention questions ("how do we do X here?"), tool or toolchain capability questions, RAG retrieval, or knowledge-card lifecycle work (search, get, add, update, draft, activate, retire, delete, validate, list, reindex, evaluate) | `.agents/skills/agent-knowledge/SKILL.md` |
```

A short boundary that keeps skills and cards from drifting into each other:

```markdown
### Skills vs Knowledge Base

- **Skills** carry always-applicable procedures: the behavior and workflow an agent must follow when doing a kind of work.
- **KB cards** carry on-demand, narrowly triggered facts and established patterns — tool, dependency, or service capabilities, project decisions, pitfalls, incidents — which may include usage guidance for that topic.
- A recurring observation or lesson that changes *how the agent should work* is promoted into a skill (or a core rule in `AGENTS.md`) when warranted. A procedure's canonical home is a skill or core rule; a card may mirror it as a retrieval aid but must not be its only home. Version-dependent facts never live in skills.
```

Write the gate and the row as the questions an agent in this repository would
actually ask. A repository of documents, runbooks, or research notes uses the
same gate and the same lifecycle wording; only the convention wording changes.

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
- Confirm the `AGENTS.md` gate and routing row above exist in this repository's
  `AGENTS.md`, and add them if they are missing.

Keep every generic section of the skill unchanged; repository-specific content
belongs only in the profile. Then run `wfm-rag index --project {{PROJECT}}` and
report the profile you wrote for review.
```

A profile that still contains `<project-specific terms>` or
`<project-specific commands>` is unfinished. Retrieval is lexical with no
synonym expansion, so those triggers decide what a future agent finds here: an
unfilled profile makes the knowledge base much harder to use.
