---
id: reuse-existing-capabilities
type: pattern
status: active
scope:
  - go
  - dependencies
triggers:
  - helper
  - parser
  - converter
  - utility
  - dependency API
  - invented helper
packages: []
paths:
  - AGENTS.md
  - src/go.mod
source_confidence: high
last_verified: 2026-07-09
---

# Reuse Existing Capabilities

## Use When

Use this card before adding helpers, parsers, converters, validators, collection utilities, config readers, or dependency-specific behavior.

## Guidance

Search local code, the standard library, and imported dependency APIs before adding custom code. Add a helper only when it owns domain behavior rather than language boilerplate.

## Verify

- Search local code for similar operations.
- Use `go doc` when package API behavior matters.
