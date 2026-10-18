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
  generated code, breaking changes), `go-deps.sh` (the dependency policy: tagged releases only, the
  minimum patched versions of `config/deps-policy.json`, then the audits the policy names), `go-security.sh`
  (`govulncheck`, OSV) and `go-integration.sh`
  (tests that need Postgres). Rung 6 is the lead realm's reviewers; rung 7 is the grader's.
- `templates/api/`: the leaf a new API starts from: a server with a health route, its contract, a
  store with migrations and `sqlc`, and the rules of the leaf in its `AGENTS.md`.
- `prompts/implementer.md`: what the implementer of a task in this leaf is told.

## Tests

`go test ./...` runs the scripts on small leaves made in a temp dir, with the real `go` and `git` and
stand-ins for the image's tools; it builds and tests the template, which needs the Go module proxy the
first time (set `YGG_REQUIRE_TEMPLATES` to fail instead of skipping when it cannot be reached). The
image itself and the tools in it are checked on a real engine (Phase 5b, v0-H14).

## CI

`.github/workflows/ci.yml` has one job, `test`, the required check on `main`. It runs in the Go image of
the devcontainer, without `go.work`, and runs `gofmt`, `go vet`, `go test -race` as a non-root user, and
`govulncheck`. It sets `YGG_REQUIRE_TEMPLATES`, so the template tests fail instead of skipping when the
module proxy cannot be reached. The pr-bot cannot change the workflow; the founder pushes changes to it.

The API template carries its own CI, `templates/api/.github/workflows/ci.yml`, so a product has a check from
its first commit. It has one job, `test`, in the same Go image, with the pack's Postgres beside it. It runs
`gofmt`, `go vet`, `golangci-lint` v2.14.0 with the template's `.golangci.yml`, `go test -race`, the
integration tests (`-tags integration`), and `govulncheck`. `TestTemplateCI` keeps it in step with the
stack: the images, the golangci-lint release and its hash are read from `devcontainer/Containerfile` and
`pack.yaml`. Rung 1 (`go-static.sh`) protects `.github/` as it does `.devcontainer/`, so only the product's
people change the workflow, never an agent in a factory run. SeaweedFS is not started: today's template has
no S3 test.

## Depends on

- Gungnir's schemas.

## Source

The v0 engineering spec, §2.1, §9, and the change proposal realm leaf naming. When this page and the
spec differ, the spec wins.
