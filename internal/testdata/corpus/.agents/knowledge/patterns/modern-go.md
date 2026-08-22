---
id: modern-go
type: pattern
status: active
scope:
  - go
triggers:
  - pointer
  - new
  - loop
  - contains
  - helper
packages: []
paths:
  - src/go.mod
  - src/apps/vex-web/handlers/user/request.go
source_confidence: high
last_verified: 2026-07-09
---

# Modern Go

## Use When

Use this card when writing or reviewing Go code, especially when the task involves pointer literals, helper functions, collection checks, or language-version-sensitive style.

## Guidance

Prefer current Go and stdlib capabilities over compatibility helpers:

- Use `new(expr)` for pointers to literal or computed values when it keeps code clear.
- Do not add `Ptr`, `StringPtr`, `BoolPtr`, or similar helper functions.
- Use stdlib collection helpers such as `slices.Contains` for simple contains checks.

## Verify

- Read `src/go.mod` for the Go version.
- Search current usage before adding helper functions.
