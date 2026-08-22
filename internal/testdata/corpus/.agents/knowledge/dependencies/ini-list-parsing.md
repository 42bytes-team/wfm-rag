---
id: ini-list-parsing
type: dependency
status: active
scope:
  - go
  - config
triggers:
  - INI
  - config
  - comma-separated
  - split
  - Strings
packages:
  - gopkg.in/ini.v1
paths:
  - src/pkgs/config/ini.go
  - src/apps/scanlan-discord/configs.go
source_confidence: high
last_verified: 2026-07-14
---

# INI List Parsing

## Use When

Use this card when a known INI key contains a delimiter-separated list.

## Guidance

Read the exact section and key, then use `Key.Strings(",")`. Keep app-specific fields in the owning app configuration rather than moving them into shared config solely to reuse parsing.

## Verify

- Read `src/pkgs/config/ini.go` and an app-specific example such as `src/apps/scanlan-discord/configs.go`.
- Run `go doc gopkg.in/ini.v1.Key.Strings` when package behavior matters.

## Avoid

Do not hand-parse comma-separated values when the product has a fixed, known key.
