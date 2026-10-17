---
title: helheim-go architecture
status: draft
date: 2026-10-17
related: [https://github.com/ouroboric-dev/yggdrasil/blob/main/docs/2-engineering/specs/2026-10-11-yggdrasil-v0-spec.md]
style: reference
---

# helheim-go architecture

The Helheim realm's Go flavor: prompts, devcontainer stack layer and services, templates and checks
for backends.

Helheim is the realm of backends, and this is its Go flavor (realm leaf naming, Yggdrasil change
proposal `2026-10-17-realm-leaf-naming`).

## Responsibilities

- Holds the recipe of a Go backend leaf (spec §9, as changed by realm leaf naming): the `implementer`
  prompt, the Go devcontainer stack layer with the services an API needs (Postgres, SeaweedFS), the
  api leaf template and its checks, under the role names every build realm shares.
- Holds no graph in v0. A run takes its graph from its lead realm (`midgard-react` for the client
  gallery); a backend-only product would add one here later.
- Its templates are copied into products, so they are MIT-licensed (`templates/`).

## What is here

- `pack.yaml`: the recipe (template, implementer prompt, the check ladder by name), the services
  (Postgres and SeaweedFS, by digest) and the sandbox: a stack built from `devcontainer/`, and the hosts
  the checks reach (the Go proxy and checksum database, the vulnerability databases).
- `devcontainer/Containerfile`: the stack layer: Go 1.26.9, `golangci-lint`, `sqlc`, `oasdiff`, `vacuum`,
  `osv-scanner` and `govulncheck`, every download checked against a recorded SHA-256, and the ladder's
  scripts in `/opt/ygg/checks`, read-only for the agent. `ygg sandbox build --realm helheim-go` builds it.
- `devcontainer/checks/`: the ladder: `go-static.sh` (gofmt, vet, lint, docs) and `go-unit.sh`,
  `go-tdd.sh` (new tests fail on the commit the task started from), `go-contract.sh` (OpenAPI lint,
  generated code, breaking changes), `go-security.sh` (`govulncheck`, OSV) and `go-integration.sh`
  (tests that need Postgres). Rung 6 is the lead realm's reviewers; rung 7 is the grader's.
- `templates/api/`: the leaf a new API starts from: a server with a health route, its contract, a
  store with migrations and `sqlc`, and the rules of the leaf in its `AGENTS.md`.
- `prompts/implementer.md`: what the implementer of a task in this leaf is told.

## Tests

`go test ./...` runs the scripts on small leaves made in a temp dir, with the real `go` and `git` and
stand-ins for the image's tools; it builds and tests the template, which needs the Go module proxy the
first time (set `YGG_REQUIRE_TEMPLATES` to fail instead of skipping when it cannot be reached). The
image itself and the tools in it are checked on a real engine (Phase 5b, v0-H14).

## Depends on

- Gungnir's schemas.

## Source

The v0 engineering spec, §2.1, §9, and the change proposal realm leaf naming. When this page and the
spec differ, the spec wins.
